// add a tracing subscriber that will map tracing outputs to the otel sdk backend
//  - this is how we get traces and metrics

use opentelemetry_appender_tracing::layer::OpenTelemetryTracingBridge;
use opentelemetry_sdk::{
    Resource,
    logs::SdkLoggerProvider,
};
use opentelemetry_otlp::{LogExporter, WithExportConfig, WithTonicConfig, tonic_types::metadata::MetadataMap};
use tracing::{warn};
use tracing_subscriber::{self, EnvFilter, Layer, layer::{SubscriberExt}, util::SubscriberInitExt};
use std::{env};

pub struct ProviderGuard {
    logger_provider: SdkLoggerProvider,
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
        let log_exporter = LogExporter::builder()
            .with_tonic()
            .with_endpoint(endpoint)
            .with_metadata(map)
            .build()
            .expect("failed to create log exporter");
        let logger_provider = SdkLoggerProvider::builder()
            .with_resource(
                Resource::builder()
                    .with_service_name("message-proxy")
                    .build(),
            )
            .with_batch_exporter(log_exporter)
            .build();
        // I don't have to set the global logger provider here because the (rust) tracing library 
        // will be integrated with the otel sdk backend via the otel tracing bridge
        ProviderGuard { logger_provider }
    }
}
impl Drop for ProviderGuard {
    // call flush / shutdown on all the providers that are wrapped by the provider guard
    fn drop(&mut self) {
        let _ = self.logger_provider.shutdown();
    }
}

pub fn init_otel() -> ProviderGuard {
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

    let level = filter_otel.max_level_hint();
    
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

    warn!("logging at level: {:?}", level);

    providers
}