// by default a module is private from it's parent modules 
// we need to declare this module as public (as well as its parent module)
// so that it can be used when setting up the server 
pub mod postgres;

use uuid::Uuid;
use std::fmt::{Display};
use trait_variant;

// inspiration for this pattern: 
// https://doc.rust-lang.org/std/io/struct.Error.html
pub enum ErrorKind {
    FailedWrite,
    SchemaMismatch,
    NotFound
}

impl Display for ErrorKind {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            Self::FailedWrite => write!(f, "FailedWrite"),
            Self::SchemaMismatch => write!(f, "SchemaMismatch"),
            Self::NotFound => write!(f, "NotFound"),
        }
    }
}

pub struct RepoError {
    pub kind: ErrorKind,
    pub source: Box<dyn std::error::Error>,
}

impl Display for RepoError {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        write!(f, "Encountered a Repository Error:\nKind: {}\nSource: {}\n", self.kind, self.source)
    }
}

// define a repository interface that can be implemented by repository structs
// this way the web socket handler code can depend on the repository interface 
// instead of depending on the repository implementation

// define domain models at the repository interface level, this way they can be shared
// between repository implementations
pub struct RepoMessage {
    pub topic_id: Uuid,
    pub user_id: Uuid,
    pub message_offset: i32,
    pub content: String,
}

// Add these super-traits
// Send: the repository needs to be able to move between threads, this is required by the tokio runtime
// Clone: the repository needs to be cloneable to that axum can pass a copy of the repository
//        struct to the handler for each invocation of the handler
// 'static: the repository must be static so that the ws.on_upgrade trait bound is satisfied. This 
//          is what guarantees the compiler that the repository does not internally contain references
//          to data that may be dropped during the lifetime of the handlers execution

// Creates a specialized version of a base trait that adds bounds to async fn and/or -> impl Trait return types.
// https://docs.rs/trait-variant/latest/trait_variant/attr.make.html
// This is quite sus, ^this project has few github stars
// we can use this macro to rewrite the trait such that the async functions are de-sugared and explicitly 
// include the trait bound on the returned future
// this is better than de-sugaring manually I guess

// This is necessary because the future returned by an async trait does not automatically 
// have the Send trait. This is required by the tokio runtime so that futures can be passed
// between cores. We use this macro to indicate that all structs that implement this trait
// must do so in such a way that the futures returned are Send.
#[trait_variant::make(Send)]
pub trait Repository: Send + Clone + 'static {
    // TODO: look into whether I should use an owned value or a string slice reference when writing to the database
    // https://doc.rust-lang.org/book/ch17-05-traits-for-async.html
    async fn write_message(&self, message: RepoMessage) -> Result<(), RepoError>;
    async fn write_messages(&self, messages: Vec<RepoMessage>) -> Result<(), RepoError>; 
}

// hopefully when we want to stub out the repository implementation when performing
// simulation testing we can use this repository interface as the interface against
// which to implement the mock repository