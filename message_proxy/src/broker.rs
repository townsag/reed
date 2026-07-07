use std::clone::Clone;
use bytes::Bytes;
use std::fmt::Display;
use std::marker::PhantomData;
use std::sync::atomic::{AtomicUsize, Ordering};
use futures::FutureExt;
use tokio::sync::broadcast::{
    self, Receiver, Sender, error::RecvError, error::SendError,
};
// use the std implementation of Mutex because we don't have to hold the lock across await points
use std::sync::{Mutex, Arc};
use std::hash::Hash;
use std::collections::HashMap;
use async_nats::{
    Client,
    PublishError,
};

// instead of passing around string literals, pass around either a reference counted
// pointer to a string or an immutable reference to a string. Not sure how the
// lifetimes would work on that one

// consider adding some idea of back pressure
// consider that this implementation might be simpler if I make the idea of a
// partition / topic id a first class citizen. Like I could make the 

// remove the clone trait bound on message by using Arc
// send reference counted pointers to messages through the channels

// const BROADCAST_CHANNEL_BUFFER_SIZE: usize = 100;
const BROADCAST_CHANNEL_BUFFER_SIZE: usize = 20_000;

pub trait ID: Eq + Hash + Clone + Display {}
impl <T: Eq + Hash + Clone + Display> ID for T {}

pub trait Routable {
    type SubjectId: ID;
    type SenderId: ID ;
    fn subject_id(&self) -> Self::SubjectId;
    fn sender_id(&self) -> Self::SenderId;
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
// TODO: figure out explicitly why these have to be public? Why does the calling code
// need to access this trait or the structs implementing the trait if they are not 
// using the BrokerBuilder type to name a variable,a etc.
pub struct Present {}
pub struct Missing {}
pub trait HasNatsClient {
    type Client;
}
impl HasNatsClient for Present {
    type Client = Client;
}
impl HasNatsClient for Missing {
    type Client = ();
}

/*
Using the builder pattern for the broker accomplishes two things:
- we can add ergonomic ways to make many configurations in the future
- we can prevent the run method of the broker from being scheduled twice
*/
// TODO: rename the build method to run to communicate that it starts an async task
// TODO: prevent the run method from being called twice using ownership rules
pub struct BrokerBuilder <N: HasNatsClient> {
    buffer_size: usize,
    nats_client: N::Client,
    state: std::marker::PhantomData<N>,
}

impl Default for BrokerBuilder<Missing> {
fn default() -> Self {
        BrokerBuilder { buffer_size: BROADCAST_CHANNEL_BUFFER_SIZE, nats_client: (), state: PhantomData }
    }
}

impl <N: HasNatsClient> BrokerBuilder <N> {
    pub fn buffer_size(mut self, buffer_size: usize) -> Self {
        self.buffer_size = buffer_size;
        self
    }
}

impl BrokerBuilder<Missing> {
    // TODO: verify that this can still be called using function chaining
    pub fn nats_client(
        self,
        nats_client: Client,
    ) -> BrokerBuilder<Present> {
        BrokerBuilder { 
            buffer_size: self.buffer_size,
            nats_client: nats_client, 
            state: PhantomData
        }
    }
}

impl BrokerBuilder<Present> {
    pub fn build<M: Routable + Clone + TryFrom<Bytes> + Into<Bytes>>(
        self,
    ) -> Broker<M> {
        Broker{
            topics: Arc::new(Mutex::new(
                HashMap::<M::SubjectId, Sender<Arc<M>>>::new()
            )),
            nats_client: self.nats_client,
            buffer_size: self.buffer_size,
        }
    }
}

pub struct WrappedSender<M: Routable + Clone + TryFrom<Bytes> + Into<Bytes>> {
    broadcast_sender: Sender<Arc<M>>,
    nats_client: Client,
}

pub enum WrappedNatsClientError {
    NatsClientFailure(PublishError),
    NatsClientSkipped,
}

// pub struct SenderMetrics {
//     // this value is only available on a successful send
//     // maybe we need to differentiate between acceptable and 
//     // critical partial failures
//     broadcast_receivers: Option<usize>,
//     len_nats_client_buff: usize,
// }

// impl <M: Routable + Clone> From<SendError<M>> for WrappedSenderError<M> {
//     fn from(value: SendError<M>) -> Self {
//         WrappedSenderError::Broadcast(value)
//     }
// }

impl <M: Routable + Clone + TryFrom<Bytes> + Into<Bytes>> WrappedSender<M> {
    /// Attempts to send the value on the broadcast channel
    /// Secondly, attempts to publish the message to the nats channel via the nats client
    /// An error type return with the try send error means the broadcast send
    /// was still successful
    /// This function is async but it will not block execution of the current task.
    /// Sending a message to the broadcast channel is non blocking.
    /// If the nats client buffer is full, we instead skip sending the message to 
    /// nats core instead of blocking.
    pub async fn send(&self, value: M) -> (Result<usize, SendError<Arc<M>>>, Result<(), WrappedNatsClientError>) {
        // ^decided to go with creating two different result types and letting the calling code differentiate
        // between them. In this case the short circuit / mutual exclusion between the result types is
        // implicit instead of explicit

        // send the value to the broadcast channel, surface any errors that 
        // are encountered here so that they may be recorded by the calling code

        // TODO: I bet I can make it so we don't need the clone trait bound on M. However, my
        // understanding of the into trait is that it strictly takes ownership of the input
        // instead of taking a reference to the input 
        let result_broadcast = self.broadcast_sender.send(Arc::new(value.clone()));
        // send the value to the nats client
        // surface errors corresponding to failure to send so they may be recorded at the calling code
        // publish is cancellation safe
        let result_nats_client = match self.nats_client
            .publish(format!("operations.{}", value.subject_id()), value.into())
            .now_or_never() {
                Some(publish_result) => {
                    publish_result.map_err(WrappedNatsClientError::NatsClientFailure)
                },
                None => Err(WrappedNatsClientError::NatsClientSkipped),
            };
        (result_broadcast, result_nats_client)
    }
}


pub struct WrappedReceiver<M: Routable> {
    topic_id: M::SubjectId,
    // TODO: modify the wrapped receiver so that clients can't clone the receiver inside the wrapped receiver
    receiver: Receiver<Arc<M>>,
    topics: Arc<Mutex<HashMap<M::SubjectId, Sender<Arc<M>>>>>,
}

impl<M: Routable> Drop for WrappedReceiver<M> {
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

impl<M: Routable> WrappedReceiver<M> {
    // implementing the recv function on the wrapped receiver and making the underlying receiver
    // private means that clients can receive from the broadcast channel without being able to
    // clone the broadcast channel. This is important because we cleanup the topic from the topic
    // hashmap while the last broadcast channel receiver is being dropped but we only put the 
    // cleanup logic in the drop function of the receiver wrapper, not in the drop function of 
    // the receiver itself 
    pub async fn recv(&mut self) -> Result<Arc<M>, RecvError> {
        return self.receiver.recv().await;
    }
}

#[derive(Clone)]
pub struct Broker<M: Routable + Clone + TryFrom<Bytes> + Into<Bytes>> {
    // mapping of topic ids to senders that can be used to signal the subscribers to a topic
    topics: Arc<Mutex<HashMap<M::SubjectId, Sender<Arc<M>>>>>,
    // nats client implements clone by itself, no need to wrap it in an arc
    nats_client: Client,
    buffer_size: usize,
}

impl<M: Routable + Clone + TryFrom<Bytes> + Into<Bytes>> Broker<M> {
    pub fn register(&self, topic_id: M::SubjectId) -> (WrappedSender<M>, WrappedReceiver<M>) {
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
            topic_id: topic_id.clone(), receiver: tx.subscribe(), topics: Arc::clone(&self.topics)
        };
        let wtx = WrappedSender {
            broadcast_sender: tx, nats_client: self.nats_client.clone(),
        };
        (wtx, wrx)
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

// TODO: make a list of all other things that need to be cleaned up

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