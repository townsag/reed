use opentelemetry_appender_tracing::layer::OpenTelemetryTracingBridge;
use opentelemetry_sdk::{
    Resource,
    logs::SdkLoggerProvider,
    metrics::SdkMeterProvider,
};
use opentelemetry_otlp::{
    LogExporter,
    MetricExporter,
    WithExportConfig,
    WithTonicConfig,
    tonic_types::metadata::MetadataMap,
};
use opentelemetry::{
    InstrumentationScope,
    KeyValue,
    global, metrics::{Counter, UpDownCounter},
};
use tracing_subscriber::{self, EnvFilter, Layer, layer::{SubscriberExt}, util::SubscriberInitExt};
use std::{env};

pub struct ProviderGuard {
    logger_provider: SdkLoggerProvider,
    meter_provider: SdkMeterProvider,
}

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
        let logger_provider = SdkLoggerProvider::builder()
            .with_resource(resource.clone())
            .with_batch_exporter(log_exporter)
            .build();
        // I don't have to set the global logger provider here because the (rust) tracing library 
        // will be integrated with the otel sdk backend via the otel tracing bridge

        let metrics_exporter = MetricExporter::builder()
            .with_tonic()
            .with_endpoint(endpoint)
            .with_metadata(map)
            .build()
            .expect("failed to create metrics exporter");
        let meter_provider = SdkMeterProvider::builder()
            .with_resource(resource)
            .with_periodic_exporter(metrics_exporter)
            .build();
        global::set_meter_provider(meter_provider.clone());

        ProviderGuard { logger_provider, meter_provider }
    }
}
impl Drop for ProviderGuard {
    // call flush / shutdown on all the providers that are wrapped by the provider guard
    fn drop(&mut self) {
        // TODO: add proper error handling here 
        let _ = self.logger_provider.shutdown();
        let _ = self.meter_provider.shutdown();
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
    let filter_otel = EnvFilter::try_from_default_env().expect("failed to parse log level env var: RUST_LOG")
        // .add_directive("hyper=off".parse().unwrap())
        // .add_directive("tonic=off".parse().unwrap())
        .add_directive("h2=off".parse().unwrap())
        .add_directive("tower=off".parse().unwrap())
        .add_directive("opentelemetry-otlp=off".parse().unwrap())
        .add_directive("opentelemetry_sdk=off".parse().unwrap());
        // .add_directive("reqwest=off".parse().unwrap());

    // let level = filter_otel.max_level_hint();
    
    let otel_logging_layer = OpenTelemetryTracingBridge::new(&providers.logger_provider)
        .with_filter(filter_otel.clone());
    let fmt_layer = tracing_subscriber::fmt::layer()
        .with_filter(filter_otel);

    tracing_subscriber::registry()
        // add the layer to the tracing subscriber registry that forwards logs from the tracing subscriber
        // to the otel logging provider
        .with(otel_logging_layer)
        // this tracing subscriber fmt layer sits at a higher level relative to the otel stdout log exporter
        // using this layer as opposed to the stdout exporter means fewer function calls and heap allocations
        .with(fmt_layer)
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