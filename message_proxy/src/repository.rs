mod postgres;

enum ErrorKind {
    FailedWrite,
    SchemaMismatch,
}

struct RepoError {
    kind: ErrorKind,
    source: Box<dyn std::error::Error>,
}

// define a repository interface that can be implemented by repository structs
// this way the web socket handler code can depend on the repository interface 
// instead of depending on the repository implementation
pub trait Repository {
    fn write_message(&self, message: &str) -> Result<(), RepoError>;
    fn write_messages(&self, messages: Vec<&str>) -> Result<(), RepoError>; 
}

// hopefully when we want to stub out the repository implementation when performing
// simulation testing we can use this repository interface as the interface against
// which to implement the mock repository