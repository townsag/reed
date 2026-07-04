use std::clone::Clone;
use std::fmt::Debug;
use std::marker::PhantomData;
use std::sync::atomic::{AtomicUsize, Ordering};
use tokio::sync::broadcast::{
    self, Receiver, Sender, error::RecvError, error::SendError,
};
use tokio::sync::mpsc::{
    Sender as MPSCSender,
    error::TrySendError,
};
use uuid::Uuid;
// use the std implementation of Mutex because we don't have to hold the lock across await points
use std::sync::{Mutex, Arc};
use std::hash::Hash;
use std::collections::HashMap;
use axum::body::Bytes;
// instead of passing around string literals, pass around either a reference counted
// pointer to a string or an immutable reference to a string. Not sure how the
// lifetimes would work on that one

// consider adding some idea of back pressure
// consider that this implementation might be simpler if I make the idea of a
// partition / topic id a first class citizen. Like I could make the 

// remove the clone trait bound on message by using Arc
// send reference counted pointers to messages through the channels

// const BUFFER_SIZE: usize = 100;
const BUFFER_SIZE: usize = 20_000;

pub trait Key: Eq + Hash + Clone + Debug {}

impl Key for Uuid {}

pub trait Routable {
    type Key: Eq + Hash + Clone + Debug;
    fn key(&self) -> &Self::Key;
}

#[derive(Clone,Debug)]
pub enum Payload {
    Text(String),
    // TODO: I don't like the idea of the broker enum depending on an
    // axum type. There must be some more generic way to represent bytes
    // that I can use here instead of axum bytes
    Binary(Bytes),
}

#[derive(Clone,Debug)]
pub struct BrokerMessage {
    pub source_id: String, 
    pub payload: Payload,
}

impl Routable for BrokerMessage {
    type Key = String;
    fn key(&self) -> &String {
        return &self.source_id;
    }
}
/*
- what is actually happening here
    - we are defining a trait called HasNatsClient and then implementing that trait on
      two empty structs Present and Missing
    - we parameterize the BrokerBuilder struct using the HasNatsClient trait and add a
      PhantomData field holding one of our empty structs
        - this allows us to know at compile time if the nats client field has been set
          on the builder and restrict some methods to only work on instances of the 
          builder that already have the nats client 
    - we create an associated type for the HasNatsClient trait indicating what the type
      of the nats_client field is 
        - this allows us to store and empty tuple in the broker builder struct when the 
          nats client is missing instead of storing an option type
        - unfortunately, this requires using generic associated types because the nats
          client is parameterized by message type
            - this is overcomplicated
- There exists an simpler way to do this
    - if we were to create two different structs, one for builder without nats client and
      one for builder with nats client, then we do not have to use the generic associated
      type to parameterize the nats_client_sender type
    - that approach requires defining the set buffer size method on each struct though
*/
struct Present {}
struct Missing {}
trait HasNatsClient {
    type Sender<M: Routable + Clone>;
}
impl HasNatsClient for Present {
    type Sender<M: Routable + Clone> = MPSCSender<M>;
}
impl HasNatsClient for Missing {
    type Sender<M: Routable + Clone> = ();
}

/*
Using the builder pattern for the broker accomplishes two things:
- we can add ergonomic ways to make many configurations in the future
- we can prevent the run method of the broker from being scheduled twice
*/
pub struct BrokerBuilder <N: HasNatsClient, M: Routable + Clone> {
    buffer_size: usize,
    nats_client_sender: N::Sender<M>,
    state: std::marker::PhantomData<N>,
}

impl <M: Routable + Clone> Default for BrokerBuilder<Missing, M> {
fn default() -> Self {
        BrokerBuilder { buffer_size: BUFFER_SIZE, nats_client_sender: (), state: PhantomData }
    }
}

impl <N: HasNatsClient, M: Routable + Clone> BrokerBuilder <N, M> {
    pub fn buffer_size(mut self, buffer_size: usize) -> Self {
        self.buffer_size = buffer_size;
        self
    }
}

impl <M: Routable + Clone> BrokerBuilder<Missing, M> {
    // TODO: verify that this can still be called using function chaining
    pub fn nats_client_sender(
        self,
        nats_client_sender: MPSCSender<M>,
    ) -> BrokerBuilder<Present, M> {
        BrokerBuilder { 
            buffer_size: self.buffer_size,
            nats_client_sender: nats_client_sender, 
            state: PhantomData
        }
    }
}

impl <M: Routable + Clone> BrokerBuilder<Present, M> {
    pub fn build<TopicId: Key>(
        self,
    ) -> Broker<TopicId, M> {
        Broker{
            topics: Arc::new(Mutex::new(
                HashMap::<TopicId, Sender<M>>::new()
            )),
            // we create a rc pointer to the nats client sender instead of copying
            // it because we do not want to increase the sender count for this 
            // mpsc channel
            nats_client_sender: Arc::new(self.nats_client_sender),
            buffer_size: self.buffer_size,
        }
    }
}

pub struct WrappedSender<TopicId: Key, M: Routable + Clone> {
    topic_id: TopicId,
    broadcast_sender: Sender<M>,
    sender_nats_client: MPSCSender<M>,
}

pub enum WrappedSenderError <M> {
    Broadcast(SendError<M>),
    NatsClient(TrySendError<M>),
}

impl <M> From<SendError<M>> for WrappedSenderError<M> {
    fn from(value: SendError<M>) -> Self {
        WrappedSenderError::Broadcast(value)
    }
}

impl <M> From<TrySendError<M>> for WrappedSenderError<M> {
    fn from(value: TrySendError<M>) -> Self {
        WrappedSenderError::NatsClient(value)
    }
}

impl <TopicId: Key, M: Routable + Clone> WrappedSender<TopicId, M> {
    /// fails with short circuit behavior to send the value on the broadcast channel
    /// Secondly tries to send the message on the mpsc channel to the nats client
    /// An error type return with the try send error means the broadcast send
    /// was still successful
    // TODO: this behavior is unintuitive, update it to return two different errors
    // or one result and one optional result
    fn send(&self, value: M) -> Result<usize, WrappedSenderError<M>> {
        // send the value to the broadcast channel, surface any errors that 
        // are encountered here so that they may be recorded by the calling code
        let count_subscribers = self.broadcast_sender.send(value.clone())?;
        // send the value to the nats client
        // surface errors corresponding to failure to send so they may be recorded at the calling code
        self.sender_nats_client.try_send(value)?;

        Ok(count_subscribers)
    }
}


pub struct WrappedReceiver<TopicId: Key, M: Routable + Clone> {
    topic_id: TopicId,
    // TODO: modify the wrapped receiver so that clients can't clone the receiver inside the wrapped receiver
    receiver: Receiver<M>,
    topics: Arc<Mutex<HashMap<TopicId, Sender<M>>>>,
}

impl<TopicId: Key, M: Routable + Clone> Drop for WrappedReceiver<TopicId, M> {
    fn drop(&mut self) {
        // upon this receiver going out of scope, we need to check if there are any remaining
        // receivers open for this topic (other than this receiver) and delete the topic from
        // the mapping if there are any other receivers

        // if the call to lock the mutex fails, that means that another thread has panicked 
        // while holding the mutex. In that case we can return
        if let Ok(mut topics) = self.topics.lock() {
            // if there are no other receivers for this topic, we should expect the receiver 
            // count to be 1 or 0. Remove the entry from the hashmap if there are no other
            // receivers
            if let Some(tx) = topics.get(&self.topic_id) && tx.receiver_count() <= 1 {
                topics.remove(&self.topic_id);
            }
        }
    }
}

impl<TopicId: Key, M: Routable + Clone> WrappedReceiver<TopicId, M> {
    // implementing the recv function on the wrapped receiver and making the underlying receiver
    // private means that clients can receive from the broadcast channel without being able to
    // clone the broadcast channel. This is important because we cleanup the topic from the topic
    // hashmap while the last broadcast channel receiver is being dropped but we only put the 
    // cleanup logic in the drop function of the receiver wrapper, not in the drop function of 
    // the receiver itself 
    pub async fn recv(&mut self) -> Result<M, RecvError> {
        return self.receiver.recv().await;
    }
}

// update the broker implementation so that they key used to identify 
// topics may be a different type than the key used to identify clients
#[derive(Clone)]
pub struct Broker<TopicId: Key, M: Routable + Clone> {
    // mapping of topic ids to senders that can be used to signal the subscribers to a topic
    topics: Arc<Mutex<HashMap<TopicId, Sender<M>>>>,
    nats_client_sender: Arc<MPSCSender<M>>,
    buffer_size: usize,
}

impl<TopicId: Key, M: Routable + Clone> Broker<TopicId, M> {
    // TODO: may have to update this so that topic_id and client_id are two different key types
    pub fn register(&self, topic_id: TopicId) -> (Sender<M>, WrappedReceiver<TopicId, M>) {
        // check the topics hashmap to see if there is already a broadcast channel for that topic_id
        let mut topics= self.topics.lock().unwrap();
        // TODO: ^this should not panic, come back to this when I understand errors well enough
        //       to return a proper error here

        // The topics list stores senders, we want to access the sender for the topic
        // id for which we are registering a connection. If the sender does not exist, then
        // we create a new sender. Either way we copy the sender from the hashmap
        let tx = topics
            // entry takes ownership because there is a chance that we will have to insert this key into
            // the map
            .entry(topic_id.clone())
            .or_insert_with(|| broadcast::channel(self.buffer_size).0)
            // you want to call .clone on channel senders to create copies of them, this is different
            // from using Arc::clone() for making new reference counted pointers. In that case we use
            // the Arc::clone() function syntax to indicate that there is no copying going on. In this
            // case we indicate that there is a copy going on with the .clone() function
            .clone();
        // create a new receiver
        // we pass &self.topics into clone because clone does not consume the original pointer
        let wrx = WrappedReceiver { 
            topic_id: topic_id, receiver: tx.subscribe(), topics: Arc::clone(&self.topics)
        };
        (tx, wrx)
    }
    /*
    // this sender will be valid until the corresponding receiver is dropped
    // the corresponding receiver is dropped when there are no more clients connected to
    // this topic_id
    pub fn get_sender(&self, topic_id: TopicId) -> Option<Sender<M>> {
        let topics = self.topics.lock().unwrap();
        topics.get(&topic_id).map(|tx| tx.clone())
    }
    */
}

pub fn get_id() -> usize {
    // using the static keyword is like declaring a piece of memory with the lifetime of 
    // the program. This memory is shared between all invocations of the get_id function
    // We use this static variable to hold an atomic integer. That way it can be called
    // in parallel from multiple threads / tasks without duplicating any ids

    // if we ever want to store these connection ids in the database instead of just using
    // them in memory we may want to change them to uuid7 instead of usize 
    static COUNTER:AtomicUsize = AtomicUsize::new(1);
    COUNTER.fetch_add(1, Ordering::Relaxed)
}