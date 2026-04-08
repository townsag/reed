use yrs::updates::encoder::Encode;
use yrs::{Update};
use yrs::updates::decoder::Decode;
use yrs::{
    StateVector,
    // encoding::read::Error,
    sync::protocol::SyncMessage,
};
use uuid::Uuid;
use std::error::Error;

use crate::repository::{Repository};

/*
// this is the enum approach, I am considering using the struct approach instead of the
// enum approach because I cannot define methods on just one variant of an enum
enum ReaderState<R: Repository> {
    /// This state is for when the websocket connection has just been created and the reader is
    /// not yet ready to receive Update operation messages. When creating the reader in the 
    /// awaiting handshake state, we also create a Last Seen Offset message and send that to 
    /// the client. Now, we are waiting for the client to send a Bulk Update message that contains
    /// all the updates that have been made on the client with a happens after relationship 
    /// relative to the last seen offset by the server
    AwaitingHandshake{repo: R},
    /// In this state, we know that we have received all messages that have been locally applied 
    /// on the server except for new in-flight messages. We wait for inflight messages to persist
    /// then broadcast to other clients
    HotPath{repo: R},
}
enum WriterState<R: Repository> {
    /// This state is for when the websocket connection has recently been created and we have
    /// not yet received a state vector from the client. We wait for the state vector so that 
    /// we can make a bulk update message and transition to the hot path state
    AwaitingHandshake{repo: R},
    /// In this state, we know that the client has received all the updates except for recent
    /// in flight updates. Wait for in flight updates so that they can be proxied to the 
    /// clientac
    HotPath{repo: R},
}
*/


// this is the struct with state type parameter approach
// this is sometimes more useful because different methods can be implemented for the state
// struct depending on the state type parameter. Furthermore, the state type can hold the 
trait WriterState {}
struct WriterAwaitingHandshake;
// ^ this could also be an enum if there were many variations of the awaiting handshake state
struct WriterHotPath {
    in_flight_operations_state_vector: StateVector,
}
impl WriterState for WriterAwaitingHandshake {}
impl WriterState for WriterHotPath {}
struct Writer <S: WriterState, R: Repository> {
    topic_id: Uuid,
    user_id: Uuid,
    client_id: u64,
    repo: R,
    state: S,
    // state: std::marker::PhantomData<S>,
}

impl<R: Repository> Writer <WriterAwaitingHandshake, R> {
    fn new(topic_id: Uuid, user_id: Uuid, client_id: u64, repo: R) -> Self {
        Writer{ topic_id, user_id, client_id, repo, state: WriterAwaitingHandshake }
    }
    // this function consumes self and returns a new instance of the writer 
    // this client version vector is expected to come from the SyncMessage::SyncStep1 variant. The resulting update is
    // expected to be used to create a SyncMessage::SyncStep2 variant
    async fn receive_state_vector(
        self, 
        mut client_version_vector: StateVector,
        // TODO: should we receive an encoded or decoded state vector here? probably encoded instead
    ) -> Result<(Writer<WriterHotPath, R>, Vec<u8>), Box<dyn Error>> {
        // read the missing messages from the database
        let pairs: Vec<(u64, u32)> = client_version_vector
            .iter()
            .map(|(k, v)| {(*k, *v)})
            .collect();
        let operations = self.repo.read_operations_after(&pairs).await?;
        let mut operations_decoded = Vec::<Update>::new();
        for op in operations {
            operations_decoded.push(Update::decode_v1(&op)?);
        }
        // construct a bulk update from the missing messages
        let bulk_update = Update::merge_updates(operations_decoded);
        client_version_vector.merge(bulk_update.state_vector());
        // return the hot path writer and the bulk update message meant to be 
        // sent over the websocket connection
        Ok((
            Writer{ 
                topic_id: self.topic_id,
                user_id: self.user_id,
                client_id: self.client_id,
                repo: self.repo, 
                state: WriterHotPath { 
                    in_flight_operations_state_vector: client_version_vector,
                }
            }, 
            bulk_update.encode_v1(),
        ))
    }
}
impl<R: Repository> Writer <WriterHotPath, R> {
    // TODO: implement methods here that are valid only when we are in the hot path state
    // For now let us have the receive update function return a HotPath writer in all cases
    // this means that we are not detecting silently dropped updates. This will have to be
    // refactored to return an enum of transitions when we want to start detecting silently
    // dropped messages
    // TODO: this may need to emit errors that cancel the reader
    fn receive_update(&mut self, update: Vec<u8>) -> Result<Vec<u8>, Box<dyn Error>> {
        // TODO: consider decoding the update elsewhere at a higher level so that we don't 
        //       have to incur the cost of decoding many times. I think I will make this
        //       change after I figure out where else we decode

        // the purpose of this function is to record internally what the latest operations 
        // the client **has been sent** from the server in the state machine. It does not 
        // perform any mutations on the update
        // compare the update received from the client with the currently known state vector

        // at some point use state vector lower to get the lower bound of this update and
        // compare that state vector to other the state vector of the last sent message to
        // detect silently dropped messages
        // https://docs.rs/yrs/latest/yrs/struct.Update.html#method.state_vector_lower

        // instead of encoding the state vector from the update then decoding the state vector
        // we should instead decode the update and access the update using update.state_vector()
        // https://docs.rs/yrs/latest/yrs/struct.Update.html#method.state_vector
        let update_decoded = Update::decode_v1(&update)?;
        let update_sv = update_decoded.state_vector();
        self.state.in_flight_operations_state_vector.merge(update_sv);
        Ok(update)
    }
    // fn receive_lagged(self) -> Writer<WriterUpdateRecovery, R>
}
impl<S: WriterState, R: Repository> Writer <S, R> {
    // these are methods that are valid on all states
}


trait ReaderState {}
struct ReaderAwaitingHandshake;
struct ReaderHotPath;

impl ReaderState for ReaderAwaitingHandshake {}
impl ReaderState for ReaderHotPath {}

struct Reader <S: ReaderState, R: Repository> {
    topic_id: Uuid,
    user_id: Uuid,
    client_id: u64,
    pub last_received_offset: u32,
    repo: R,
    state: S,
}
impl<R: Repository> Reader<ReaderAwaitingHandshake, R> {
    async fn new(repo: R, topic_id: Uuid, user_id: Uuid, client_id: u64) -> Result<Self, Box<dyn Error>> {
        // read the last received update offset from this client from the database
        let last_received_offset = repo.read_last_received_offset(client_id).await?;
        // create a reader instance and return it
        Ok(Reader{
            topic_id, user_id, client_id, last_received_offset, repo, state: ReaderAwaitingHandshake,
        })
    }
    // TODO: may have to change this return error type to be a better error
    async fn receive_bulk_update(
        self, 
        client_bulk_update: Vec<u8>,
    ) -> Result<(Reader<ReaderHotPath, R>, Vec<u8>), Box<dyn Error>> {
        let update = Update::decode_v1(&client_bulk_update)?;
        let new_offset = update.state_vector().get(&self.client_id);
        // persist the bulk update that we have received
        self.repo.write_operation(
            self.topic_id, self.user_id, self.client_id,
            new_offset, &client_bulk_update,
        ).await?;
        // update the last received offset
        // return a hot path reader
        Ok((Reader{
            topic_id: self.topic_id,
            user_id: self.user_id,
            client_id: self.client_id,
            last_received_offset: new_offset,
            repo: self.repo,
            state: ReaderHotPath,
        }, client_bulk_update))
    }
}
impl<R: Repository> Reader<ReaderHotPath, R> {
    async fn receive_update(&mut self, update: Vec<u8>) -> Result<Vec<u8>, Box<dyn Error>> {
        // persist the update to the database 
        let update_decoded = Update::decode_v1(&update)?;
        let new_offset = update_decoded.state_vector().get(&self.client_id);
        self.repo.write_operation(
            self.topic_id, self.user_id, self.client_id, new_offset, &update,
        ).await?;
        // update the last received offset
        self.last_received_offset = new_offset;
        // return the update so that it may be passed to the broadcast queue
        Ok(update)
    }
}