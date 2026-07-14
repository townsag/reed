/* 
you can use this approach to view the generated code
- select include!
- command + p
- > rust-analyzer: Expand macro recursively at caret
*/
include!(concat!(env!("OUT_DIR"), "/api.operations.rs"));

use prost::{DecodeError, Message};
use uuid::Uuid;
use bytes::Bytes;
use std::io::Cursor;
use crate::broker::{Routable, ToBytes};

impl Routable for Operation {
    type SenderId = u64;
    type SubjectId = Uuid;
    
    fn sender_id(&self) -> Self::SenderId { self.client_id }
    fn subject_id(&self) -> Self::SubjectId {
        Uuid::from_u64_pair(self.document_id_high, self.document_id_low)
    }
}
impl TryFrom<Bytes> for Operation {
    type Error = DecodeError;
    fn try_from(value: Bytes) -> Result<Self, Self::Error> {
        // TODO: figure out what this does explicitly
        Operation::decode(&mut Cursor::new(value))
    }
}
impl ToBytes for Operation {
    fn to_bytes(&self) -> Bytes {
        let mut buf = Vec::new();
        buf.reserve(self.encoded_len());
        self.encode(&mut buf).unwrap();
        buf.into()
    }
}
impl Operation {
    pub fn new(
        document_id: Uuid,
        client_id: u64,
        offset: Option<u32>,
        payload: Vec<u8>,
        has_deletion: bool,
    ) -> Self {
        let (document_id_high, document_id_low) = document_id.as_u64_pair();
        Operation { 
            document_id_high, document_id_low, 
            client_id, offset, payload, has_deletion
        }
    }
}