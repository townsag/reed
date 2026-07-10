use std::clone::Clone;
use bytes::Bytes;
use std::fmt::Display;
use std::marker::PhantomData;
use std::sync::atomic::{AtomicUsize, Ordering};
use futures::{FutureExt, StreamExt};
use tokio::sync::broadcast::{
    self, Receiver, Sender, error::RecvError, error::SendError,
};
use tokio::task::{
    JoinHandle,
};
// use the std implementation of Mutex because we don't have to hold the lock across await points
use std::sync::{Mutex, Arc};
use std::hash::Hash;
use std::collections::{
    hash_map::Entry::{Occupied, Vacant},
    HashMap,
};
use async_nats::{
    Client,
    PublishError,
    Subscriber,
};
use tracing::{event, Level};

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

pub trait ID: Eq + Hash + Clone + Display + Send + 'static {}
impl <T: Eq + Hash + Clone + Display + Send + 'static> ID for T {}

pub trait Routable {
    type SubjectId: ID;
    type SenderId: ID ;
    fn subject_id(&self) -> Self::SubjectId;
    fn sender_id(&self) -> Self::SenderId;
}

pub trait ToBytes {
    fn to_bytes(&self) -> Bytes;
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

pub trait Message: Routable + TryFrom<Bytes, Error: std::fmt::Display> + ToBytes + Send + Sync + 'static {}
impl <T: Routable + TryFrom<Bytes, Error: std::fmt::Display> + ToBytes + Send + Sync + 'static> Message for T {}

impl BrokerBuilder<Present> {
    pub fn build<M: Message>(
        self,
    ) -> Broker<M> {
        Broker{
            topics: Arc::new(Mutex::new(
                HashMap::<M::SubjectId, TopicScopedState<M>>::new()
            )),
            nats_client: self.nats_client,
            buffer_size: self.buffer_size,
        }
    }
}

pub struct WrappedSender<M: Message> {
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

impl <M: Message> WrappedSender<M> {
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
        let (subject_id, payload) = (value.subject_id(), value.to_bytes());
        // send the value to the broadcast channel, surface any errors that 
        // are encountered here so that they may be recorded by the calling code
        let result_broadcast = self.broadcast_sender.send(Arc::new(value));
        // send the value to the nats client
        // surface errors corresponding to failure to send so they may be recorded at the calling code
        // publish is cancellation safe
        let result_nats_client = match self.nats_client
            .publish(format!("operations.{}", subject_id), payload)
            .now_or_never() {
                Some(publish_result) => {
                    publish_result.map_err(WrappedNatsClientError::NatsClientFailure)
                },
                None => Err(WrappedNatsClientError::NatsClientSkipped),
            };

        (result_broadcast, result_nats_client)
    }
}


pub struct WrappedReceiver<M: Message> {
    topic_id: M::SubjectId,
    // TODO: modify the wrapped receiver so that clients can't clone the receiver inside the wrapped receiver
    receiver: Receiver<Arc<M>>,
    topics: Arc<Mutex<HashMap<M::SubjectId, TopicScopedState<M>>>>,
}

impl<M: Message> Drop for WrappedReceiver<M> {
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
            if let Some(state) = topics.get(&self.topic_id) && state.broadcast_sender.receiver_count() <= 1 {
                topics.remove(&self.topic_id);
            }
        }
    }
}

impl<M: Message> WrappedReceiver<M> {
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

struct TopicScopedState<M: Message> {
    broadcast_sender: Sender<Arc<M>>,
    nats_core_subscriber_task: JoinHandle<()>,
}

impl <M: Message> Drop for TopicScopedState<M> {
    fn drop(&mut self) {
        self.nats_core_subscriber_task.abort();
    }
}

// - The send trait bound here indicates that ownership of the message value can be transferred
//   between threads
//      - requires either synchronization or lifetimes
// - the sync trait indicates that multiple threads can access the value at the same time
//      - requires synchronization
// - the static lifetime indicates that the message isn't made up of borrowed references
//   to data that may be owned elsewhere and dropped
impl <M: Message> TopicScopedState<M> {
    fn new(topic_id: M::SubjectId, buffer_size: usize, mut sub: Subscriber) -> Self {
        // create the broadcast channel
        let tx: Sender<Arc<M>> = broadcast::channel(buffer_size).0;
        let broadcast_sender = tx.clone();
        // create the async task that polls the nats core subscriber and published messages
        // messages to the broadcast channel
        let middleware_handle = tokio::spawn(async move {
            loop {
                // read from the subscriber
                match sub.next().await {
                    Some(msg) => {
                        // write to the sender
                        let parsed = M::try_from(msg.payload);
                        match parsed {
                            Ok(value) => {
                                if let Err(_) = tx.send(Arc::new(value)) {
                                    event!(Level::WARN, topic_id=%topic_id, "failed to write message to broadcast sender");
                                    return
                                }
                            },
                            Err(e) => {
                                event!(
                                    Level::WARN, %topic_id, error=%e,
                                    "failed to deserialize message received from nats core subscriber",
                                );
                                continue
                            }
                        }
                    },
                    None => return
                }
            }
        });
        // return self
        TopicScopedState { broadcast_sender: broadcast_sender, nats_core_subscriber_task: middleware_handle }
    }
    fn clone_sender(&self) -> Sender<Arc<M>> {
        self.broadcast_sender.clone()
    }
}

#[derive(Clone)]
pub struct Broker<M: Message> {
    // mapping of topic ids to senders that can be used to signal the subscribers to a topic
    topics: Arc<Mutex<HashMap<M::SubjectId, TopicScopedState<M>>>>,
    // nats client implements clone by itself, no need to wrap it in an arc
    nats_client: Client,
    buffer_size: usize,
}

impl<M: Message> Broker<M> {
    pub async fn register(&self, topic_id: M::SubjectId) -> Result<(WrappedSender<M>, WrappedReceiver<M>), async_nats::Error> {
        // check the topics hashmap to see if there is already a broadcast channel for that topic_id
        let mut topics= self.topics.lock().unwrap();
        // TODO: ^this should not panic, come back to this when I understand errors well enough
        //       to return a proper error here

        // The topics list stores senders, we want to access the sender for the topic
        // id for which we are registering a connection. If the sender does not exist, then
        // we create a new sender. Either way we copy the sender from the hashmap
        let tx = match topics.entry(topic_id.clone()) { 
            Occupied(entry) => entry.get().broadcast_sender.clone(),
            Vacant(entry) => {
                let sub = self.nats_client.subscribe(format!("operations.{}", topic_id)).await?;
                let state = TopicScopedState::new(topic_id.clone(), self.buffer_size, sub);
                entry.insert(state).clone_sender()
            },
        };

        // create a new receiver
        // we pass &self.topics into clone because clone does not consume the original pointer
        let wrx: WrappedReceiver<M> = WrappedReceiver { 
            topic_id: topic_id, receiver: tx.subscribe(), topics: Arc::clone(&self.topics)
        };
        let wtx = WrappedSender {
            broadcast_sender: tx, nats_client: self.nats_client.clone(),
        };
        
        Ok((wtx, wrx))
    }
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