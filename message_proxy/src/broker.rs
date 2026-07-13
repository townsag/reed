use std::clone::Clone;
use bytes::Bytes;
use std::fmt::Display;
use std::marker::PhantomData;
use std::sync::atomic::{AtomicUsize, Ordering};
use futures::{FutureExt, StreamExt};
use tokio::sync::broadcast::{
    self, 
    Receiver as BroadcastReceiver, 
    Sender as BroadcastSender, 
    error::RecvError, error::SendError,
};
use tokio::sync::mpsc::{
    self, Receiver, Sender, UnboundedReceiver, UnboundedSender,
};
use tokio::sync::oneshot::{
    self, Sender as ResponseSender,
};
use tokio::task::{
    JoinHandle,
};
// use the std implementation of Mutex because we don't have to hold the lock across await points
use std::sync::{Arc};
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
// TODO: do some math about the timeout / upper bound on the creation of the 
// nats subscriber and the number of new clients per second this channel will
// be able to accommodate
const TOPIC_STATE_CHANNEL_BUFFER_SIZE: usize = 10;

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
            topic_state_actor_handler: TopicsStateActorHandle::new(self.nats_client, self.buffer_size),
        }
    }
}

pub struct WrappedSender<M: Message> {
    broadcast_sender: BroadcastSender<Arc<M>>,
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
    receiver: BroadcastReceiver<Arc<M>>,
    deregister_sender: UnboundedSender<DeregisterRequest<M>>,
}

impl<M: Message> Drop for WrappedReceiver<M> {
    fn drop(&mut self) {
        // upon this receiver going out of scope, we need to indicate to the actor that manages subject
        // scoped state that one of the 
        // TODO: remember that the drop function executes before the data inside of the struct is dropped
        // that means that the receiver wrapped by the struct may still be alive when the actor receives 
        // the deregister message. The actor should account for this
        self.deregister_sender.send(DeregisterRequest { topic_id: self.topic_id.clone() });
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
    broadcast_sender: BroadcastSender<Arc<M>>,
    nats_core_subscriber_task: JoinHandle<()>,
    count_registered: usize,
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
    fn new(topic_id: M::SubjectId, broadcast_buffer_size: usize, mut sub: Subscriber) -> Self {
        // create the broadcast channel
        let tx: BroadcastSender<Arc<M>> = broadcast::channel(broadcast_buffer_size).0;
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
        TopicScopedState { 
            broadcast_sender: broadcast_sender, 
            nats_core_subscriber_task: middleware_handle, 
            count_registered: 0
        }
    }
    // if we tie creating a broadcast sender to the act of incrementing the count of registered
    // tasks then we can prevent broadcast senders from being created without registering
    fn register(&mut self) -> BroadcastSender<Arc<M>> {
        self.count_registered += 1;
        self.broadcast_sender.clone()
    }
    fn decrement_registered(&mut self) {
        self.count_registered -= 1;
    }
}

// enum TopicsStateActorMessage <M: Message> {
//     Register(M::SubjectId, ResponseSender<(WrappedSender<M>, WrappedReceiver<M>)>),
//     Deregister(M::SubjectId),
// }
struct RegisterRequest <M: Message> {
    topic_id: M::SubjectId,
    response_channel: ResponseSender<Result<(WrappedSender<M>, WrappedReceiver<M>), async_nats::Error>>,
}
struct DeregisterRequest <M: Message> {
    topic_id: M::SubjectId,
}

struct TopicsStateActor <M: Message> {
    register_receiver: Receiver<RegisterRequest<M>>,
    deregister_sender: UnboundedSender<DeregisterRequest<M>>,
    deregister_receiver: UnboundedReceiver<DeregisterRequest<M>>,

    topics: HashMap<M::SubjectId, TopicScopedState<M>>,
    // nats client implements clone by itself, no need to wrap it in an arc
    nats_client: Client,
    broadcast_buffer_size: usize,
}

impl <M: Message> TopicsStateActor <M> {
    // TODO: why would this function need to take mut& self?
    async fn run(mut self) {
        let mut register_enabled= true;
        loop {
            let deregister_enabled = self.deregister_sender.strong_count() > 1 && !self.deregister_sender.is_closed();
            tokio::select! {
                req = self.register_receiver.recv(), if register_enabled => match req {
                    Some(reg) => { self.handle_register(reg.topic_id, reg.response_channel).await; },
                    None => { 
                        // this case corresponds to the last instance of the handler being dropped 
                        // if the last instance of the handler is dropped that means that there are 
                        // no longer any more tasks that can call the register() method on the handler
                        register_enabled = false;
                    }
                },
                /*
                - Decided to go with the approach of having a strong unbounded sender inside of the 
                  topics state actor
                    - this allows the deregister sender to be copied into the wrapped receiver inside
                      of the handler register function. This prevents a bug where the broadcast receiver
                      is dropped before it is added to the dropped receiver, resulting in potential 
                      skipped cleanup of the broadcast channel
                    - if the deregister sender was a weak sender instead of a strong sender then the
                      deregister channel could be prematurely closed if it was polled but there were
                      no senders
                This feels mildly overcomplicated. If there is another way to do this that is easier
                then we should do it that way 
                 */
                req = self.deregister_receiver.recv(), if deregister_enabled => match req {
                    Some(dereg) => { self.handle_deregister(dereg.topic_id); },
                    None => { /* we will detect that the channel is closed at top of loop */ }
                }, 
                /*
                If the register channel has been closed because there are no more copies of the handler
                and the copy of the deregister sender held by the topics state actor is the only
                remaining copy of the topics state actor, then we need to stop the topics state
                actor run function. This results in cleanup on the topic state actors owned values,
                including the topic scoped state.
                 */
                else => return
            }
        }
    }
    async fn handle_register(
        &mut self, 
        topic_id: M::SubjectId, 
        os_channel: ResponseSender<Result<(WrappedSender<M>, WrappedReceiver<M>), async_nats::Error>>,
    ) {
        // if there is no entry for this topic_id in the topics hashmap, add one
        // the entry holds a copy of the broadcast sender and the handle to the
        // tokio task which reads from the nats subscriber and writes to the 
        // broadcast sender
        let tx = match self.topics.entry(topic_id.clone()) {
            Occupied(entry) => {
                let mut state = entry.get();
                state.register()
            },
            Vacant(entry) => {
                // send the error as a response over the oneshot channel
                let sub = match self.nats_client.subscribe(format!("operations.{}", topic_id)).await {
                    Ok(sub) => sub,
                    Err(e) => {
                        // TODO: send the error over the oneshot channel
                        os_channel.send(Err(e));
                        return
                    }
                };
                let topic_state = TopicScopedState::new(topic_id, self.broadcast_buffer_size, sub);
                entry.insert(topic_state).register()
            },
        };
        /*
        It is imperative that we create the wrapped receiver here instead of in the calling code because
        we want to avoid the potential situation in which the broadcast receiver is dropped before it 
        is wrapped in the wrapped receiver. This could lead to leaked topic scoped state / memory.
         */
        let wrx = WrappedReceiver {
            topic_id: topic_id, receiver: tx.subscribe(), deregister_sender: self.deregister_sender.clone(),
        };
        let wtx = WrappedSender {
            broadcast_sender: tx, nats_client: self.nats_client.clone(),
        };
        // send the sender and receiver over the oneshot response channel. We are not concerned with the
        // result. Either it succeeds or it fails and the relevant housekeeping happens in the wrapped
        // receivers drop function.
        let _ = os_channel.send(Ok((wtx, wrx)));
    }
    fn handle_deregister(&mut self, topic_id: M::SubjectId) {
        // if there is an entry for this topic_id 
        // check the receiver count for the broadcast sender in the topic scoped state
        // if the receiver count is 0, drop the topic scoped state by removing
        // that entry from the hashmap
        /*
        Using the broadcast sender receiver count can result in a race condition between the 
        drop function of the wrapped receiver and the handle deregister function. That is why
        we explicitly count the number of registered tasks inside the topic scoped state
        instead of relying on the count of the broadcast sender
         */
        if let Occupied(mut e) = self.topics.entry(topic_id) {
            let state = e.get_mut();
            if state.count_registered == 1 {
                e.remove();
            } else {
                state.decrement_registered();
            }
        }
    }
}

#[derive(Clone)]
struct TopicsStateActorHandle <M: Message> {
    register_sender: Sender<RegisterRequest<M>>,
    deregister_sender: UnboundedSender<DeregisterRequest<M>>,
}

impl <M: Message> TopicsStateActorHandle<M> {
    fn new(nats_client: Client, broadcast_buffer_size: usize) -> Self {
        let (register_tx, register_rx) = mpsc::channel(TOPIC_STATE_CHANNEL_BUFFER_SIZE);
        let (deregister_tx, deregister_rx) = mpsc::unbounded_channel();
        let actor = TopicsStateActor {
            register_receiver: register_rx,
            deregister_sender: deregister_tx.clone(),
            deregister_receiver: deregister_rx,
            topics: HashMap::new(),
            nats_client,
            broadcast_buffer_size: broadcast_buffer_size,
        };
        tokio::spawn(async move {
            actor.run().await;
        });
        TopicsStateActorHandle { register_sender: register_tx, deregister_sender: deregister_tx }
    }
    async fn register(&self, topic_id: M::SubjectId) -> Result<(WrappedSender<M>, WrappedReceiver<M>), async_nats::Error> {
        let (response_tx, response_rx) = oneshot::channel();
        self.register_sender.send(RegisterRequest { topic_id: topic_id, response_channel: response_tx }).await;
        // TODO: add a timeout here, either implicit or explicit
        match response_rx.await {
            Ok(result) => {
                return result
            },
            // this means that the sender is dropped without sending. This indicates that the actor has failed 
            // critically at some point
            Err(e) => {
                // TODO: look into the anyhow code to understand how this is supposed to work
            },
        }
    }
}

#[derive(Clone)]
pub struct Broker<M: Message> {
    topic_state_actor_handler: TopicsStateActorHandle<M>,
}

impl<M: Message> Broker<M> {
    pub async fn register(&self, topic_id: M::SubjectId) -> Result<(WrappedSender<M>, WrappedReceiver<M>), async_nats::Error> {
        self.topic_state_actor_handler.register(topic_id).await
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