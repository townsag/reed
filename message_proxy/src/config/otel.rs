use opentelemetry_appender_tracing::layer::OpenTelemetryTracingBridge;
use opentelemetry_sdk::{
    Resource,
    logs::{BatchConfigBuilder, BatchLogProcessor, SdkLoggerProvider},
    metrics::{PeriodicReader, SdkMeterProvider},
    trace::SdkTracerProvider,
};
use opentelemetry_otlp::{
    LogExporter,
    MetricExporter,
    SpanExporter,
    WithExportConfig,
    WithTonicConfig,
    tonic_types::metadata::MetadataMap,
};
use opentelemetry::{
    InstrumentationScope,
    KeyValue,
    global, metrics::{Counter, UpDownCounter}, trace::TracerProvider,
};
use tracing::{
    Level, field::{Field, Visit}
};
use tracing_subscriber::{
    self, EnvFilter, Layer, layer::{self, SubscriberExt}, util::SubscriberInitExt
};
use tracing_opentelemetry;
use std::{env, time::Duration};


#[derive(Clone)]
pub struct MetricsWS {  
    count_websocket_connections: UpDownCounter<i64>,
    count_received_messages: Counter<u64>,
    count_received_contents: Counter<u64>,
}
#[derive(Debug)]
pub enum WSMessageType {
    SyncStep1,
    SyncStep2,
    Update,
    Error,
}

pub struct WsLifecycleGuard {
    count_websocket_connections: UpDownCounter<i64>,
}

impl Drop for WsLifecycleGuard {
    fn drop(&mut self) {
        self.count_websocket_connections.add(-1, &[]);
    }
}

impl MetricsWS {
    pub fn ws_lifecycle_guard(&self) -> WsLifecycleGuard {
        // TODO: add an attribute containing the hostname of this server
        self.count_websocket_connections.add(1, &[]);
        // I think this clone is fine because under the hood the UpDownCounter is just
        // an Arc
        WsLifecycleGuard { count_websocket_connections: self.count_websocket_connections.clone() }
    }
    pub fn record_received_ws_message(&self, size_bytes: usize, message_type: WSMessageType) {
        let attributes = [KeyValue::new("message.type", format!("{:?}", message_type))];
        self.count_received_messages.add(1, &attributes);
        self.count_received_contents.add(size_bytes as u64, &attributes);
    }
}

pub struct ProviderGuard {
    logger_provider: SdkLoggerProvider,
    meter_provider: SdkMeterProvider,
    tracer_provider: SdkTracerProvider,
}


impl ProviderGuard {
    // init the various sdk parts
    //  - init tracer
    //  - init metrics
    //  - init logs
    fn new() -> Self {
        // TODO: read endpoint from the env
        let endpoint = env::var("OTEL_EXPORTER_OTLP_ENDPOINT")
            .expect("failed to read OTEL_EXPORTER_OTLP_ENDPOINT from os env");
        let hyperdx_api_key = env::var("HYPERDX_INGESTION_KEY")
            .expect("failed to read HYPERDX_INGESTION_KEY from os env");
        let mut map = MetadataMap::with_capacity(1);
        map.insert("authorization", hyperdx_api_key.parse().unwrap());
        // resource denotes the physical infra that created the telemetry
        // this is where I would put things like pod number etc.
        let resource = Resource::builder()
            .with_service_name("message-proxy")
            .build();

        let log_exporter = LogExporter::builder()
            .with_tonic()
            .with_endpoint(endpoint.clone())
            .with_metadata(map.clone())
            .build()
            .expect("failed to create log exporter");
        let log_processor = BatchLogProcessor::builder(log_exporter)
            .with_batch_config(
                BatchConfigBuilder::default()
                    .with_max_export_batch_size(512)
                    .with_max_queue_size(8192)
                    .build()
            )
            .build();
        let logger_provider = SdkLoggerProvider::builder()
            .with_resource(resource.clone())
            .with_log_processor(log_processor)
            .build();
        // I don't have to set the global logger provider here because the (rust) tracing library 
        // will be integrated with the otel sdk backend via the otel tracing bridge

        let span_exporter = SpanExporter::builder()
            .with_tonic()
            .with_endpoint(endpoint.clone())
            .with_metadata(map.clone())
            .build()
            .expect("failed to create span exporter");
        let tracer_provider = SdkTracerProvider::builder()
            .with_resource(resource.clone())
            .with_batch_exporter(span_exporter)
            .build();
        // Set the global tracer provider using a clone of the tracer_provider.
        // Setting global tracer provider is required if other parts of the application
        // uses global::tracer() or global::tracer_with_version() to get a tracer.
        // Cloning simply creates a new reference to the same tracer provider.
        global::set_tracer_provider(tracer_provider.clone());

        let metrics_exporter = MetricExporter::builder()
            .with_tonic()
            .with_endpoint(endpoint)
            .with_metadata(map)
            .build()
            .expect("failed to create metrics exporter");
        let reader = PeriodicReader::builder(metrics_exporter)
            .with_interval(Duration::from_secs(5))
            .build();
        let meter_provider = SdkMeterProvider::builder()
            .with_resource(resource)
            .with_reader(reader)
            // .with_periodic_exporter(metrics_exporter)
            .build();
        global::set_meter_provider(meter_provider.clone());

        ProviderGuard { logger_provider, meter_provider, tracer_provider }
    }
}
impl Drop for ProviderGuard {
    // call flush / shutdown on all the providers that are wrapped by the provider guard
    fn drop(&mut self) {
        // TODO: add proper error handling here 
        let _ = self.logger_provider.shutdown();
        let _ = self.meter_provider.shutdown();
        let _ = self.tracer_provider.shutdown();
    }
}

struct ClientIdExtractor{
    client_id_src: Option<u64>
}

impl ClientIdExtractor {
    fn new() -> Self {
        ClientIdExtractor{ client_id_src: None }
    }
}

impl Visit for ClientIdExtractor {
    fn record_debug(&mut self, _field: &Field, _value: &dyn core::fmt::Debug) {}
    fn record_u64(&mut self, field: &Field, value: u64) {
        if field.name() == "client_id_src" {
            self.client_id_src = Some(value)
        }
    }
}

struct ClientLayerFilter{
    filtering_enabled: bool
}

impl <S> layer::Filter<S> for ClientLayerFilter {
    fn enabled(
        &self,
        _meta: &tracing::Metadata<'_>,
        _cx: &layer::Context<'_,S>,
    ) -> bool {
        return true;
    }
    fn event_enabled(
        &self,
        event: &tracing::Event<'_>,
        _ctx: &tracing_subscriber::layer::Context<'_, S>,
    ) -> bool {
        // allow all events with severity level higher than info
        if *event.metadata().level() <= Level::WARN || !self.filtering_enabled {
            return true;
        }
        // need to construct a visitor that records the client_id_src field of the log 
        // https://docs.rs/tracing-core/latest/tracing_core/field/trait.Visit.html
        let mut visitor = ClientIdExtractor::new();
        event.record(&mut visitor);
        
        match visitor.client_id_src {
            // allow all events with client_id_src values that evenly divide by 10
            Some(client_id) => { return client_id % 10 == 0; },
            // allow all events with no client_id_src field
            None => { return false; }
        }
    }
}

pub fn init_otel() -> (ProviderGuard, MetricsWS) {
    let providers = ProviderGuard::new();
    // add a opentelemetry_appender_tracing tracing subscriber layer that will map tracing events 
    // to the otel backend
    //  - this is how we get logs
    // https://docs.rs/opentelemetry-appender-tracing/0.31.1/opentelemetry_appender_tracing/

    // copied from https://github.com/open-telemetry/opentelemetry-rust/blob/main/opentelemetry-otlp/examples/basic-otlp/src/main.rs
    // To prevent a telemetry-induced-telemetry loop, OpenTelemetry's own internal
    // logging is properly suppressed. However, logs emitted by external components
    // (such as reqwest, tonic, etc.) are not suppressed as they do not propagate
    // OpenTelemetry context. Until this issue is addressed
    // (https://github.com/open-telemetry/opentelemetry-rust/issues/2877),
    // filtering like this is the best way to suppress such logs.
    //
    // The filter levels are set as follows:
    // - Allow `info` level and above by default.
    // - Completely restrict logs from `hyper`, `tonic`, `h2`, and `reqwest`.
    //
    // Note: This filtering will also drop logs from these components even when
    // they are used outside of the OTLP Exporter.
    // A span or event will be recorded if it is enabled by any per-layer filter, but it will be skipped
    // by the layers whose filters did not enable it.
    let filter_otel = EnvFilter::try_from_default_env()
        .expect("failed to parse log level env var: RUST_LOG")
        // .add_directive("hyper=off".parse().unwrap())
        // hyper_util
        // .add_directive("tonic=off".parse().unwrap())
        // .add_directive("reqwest=off".parse().unwrap());
        // === start directives we may want ===
        .add_directive("async_nats=info".parse().unwrap())
        .add_directive("h2=info".parse().unwrap())
        .add_directive("tower=info".parse().unwrap())
        .add_directive("opentelemetry-otlp=info".parse().unwrap())
        .add_directive("opentelemetry_sdk=info".parse().unwrap());
        // === end directives we may want =====
    /*
    Checkpoint:
    - there are logs missing
    - it looks like we are occasionally failing to export telemetry from the message proxy task to the otel collector
    - in order to diagnose this problem, adjust the message proxy fmt logs to log otel sdk logs at the DEBUG level 
      but log message proxy application related information at the WARN level
     */
    // filtering with a target will filter an event by default then only include the event if it 
    // passes the target filter. This is different than the behavior that we want. Instead we want
    // to include the event by default then filter out events 
    let mp_module_log_level = env::var("RUST_LOG_MP_MODULE").unwrap_or("warn".into());
    let filter_fmt = EnvFilter::try_from_default_env()
        .expect("failed to parse log level env var: RUST_LOG")
        .add_directive(format!("message_proxy={}", mp_module_log_level).parse().unwrap())
        .add_directive("sqlx=warn".parse().unwrap())
        .add_directive("async_nats=info".parse().unwrap())
        .add_directive("h2=info".parse().unwrap())
        .add_directive("tower=info".parse().unwrap())
        .add_directive("opentelemetry-otlp=info".parse().unwrap())
        .add_directive("opentelemetry_sdk=info".parse().unwrap());

    let client_id_filtering_enabled = env::var("CLIENT_ID_FILTERING_ENABLED")
        .unwrap_or("false".into())
        .parse::<bool>()
        .expect("failed to parse CLIENT_ID_FILTERING_ENABLED env var, must be one of 'true' or 'false'");
    let client_sample_filter = ClientLayerFilter{
        filtering_enabled: client_id_filtering_enabled,
    };
    
    // set up logging related otel <--> rust machinery
    let otel_logging_layer = OpenTelemetryTracingBridge::new(&providers.logger_provider)
        .with_filter(filter_otel)
        // this layer removed logs that have client_id and are not Warn or higher and are from client_ids that
        // are not sampled
        .with_filter(client_sample_filter);
    let fmt_layer = tracing_subscriber::fmt::layer()
        .with_filter(filter_fmt);
        // .with_filter(filter_fmt_message_proxy);

    // set up tracing related otel <--> rust machinery
    let tracer = providers.tracer_provider.tracer("mp-service");
    let otel_tracing_layer = tracing_opentelemetry::layer().with_tracer(tracer);

    tracing_subscriber::registry()
        // this tracing subscriber fmt layer sits at a higher level relative to the otel stdout log exporter
        // using this layer as opposed to the stdout exporter means fewer function calls and heap allocations
        .with(fmt_layer)
        // add the layer to the tracing subscriber registry that forwards logs from the tracing subscriber
        // to the otel logging provider
        .with(otel_logging_layer)
        // this layer intercepts traces being sent to the rust tracing library and forwards them to the otel sdk
        .with(otel_tracing_layer)
        // Attempts to set self as the global default subscriber in the current scope, panicking if this fails.
        // In this case the scope means the entire process, compared to using a tracing subscriber in a limited
        // scope using closures
        .init();

    // create the metrics struct so that it may be passed around at the application level
    // scope denotes logical information about the software running on physical hardware
    // this is where I would put things like version number, git hash, library name, etc.
    // let common_scope_attributes = vec![KeyValue::new("scope-key", "scope-value")];
    let scope = InstrumentationScope::builder("message-proxy")
        .with_version("v0.0.1")
        .build();
    let meter = global::meter_with_scope(scope);
    // {namespace}.{component}.{action_or_measurement}
    let count_websocket_connections = meter
        .i64_up_down_counter("mp-service.ws.count-connected-clients")
        .with_description("count of currently connected websocket clients for the operation proxy endpoint")
        .build();
    let count_received_messages = meter
        .u64_counter("mp-service.ws.count-received-messages")
        .with_description("count of websocket messages received")
        .with_unit("message")
        .build();
    let count_received_contents = meter
        .u64_counter("mp-service.ws.count-received-contents")
        .with_description("count of bytes received across all websocket messages")
        .with_unit("bytes")
        .build();
    let metrics_ws = MetricsWS {
        count_websocket_connections,
        count_received_messages,
        count_received_contents,
    };
    (providers, metrics_ws)
}