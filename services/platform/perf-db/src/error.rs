use thiserror::Error;

#[derive(Debug, Error)]
pub enum PerfDbError {
    #[error("I/O error ({context}): {source}")]
    Io {
        context: String,
        #[source]
        source: std::io::Error,
    },

    #[error("serialization error ({context}): {detail}")]
    Serialization { context: String, detail: String },

    #[error("data corruption: {0}")]
    Corruption(String),

    #[error("invalid argument: {0}")]
    InvalidArgument(String),
}
