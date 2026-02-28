use std::clone::Clone;
use std::sync::atomic::{AtomicUsize, Ordering};
use tokio::sync::broadcast::{
    self, Receiver, Sender, error::RecvError,
};
// use the std implementation of Mutex because we don't have to hold the lock across await points
use std::sync::{Mutex, Arc};
use std::hash::Hash;
use std::collections::HashMap;

// instead of passing around string literals, pass around either a reference counted
// pointer to a string or an immutable reference to a string. Not sure how the
// lifetimes would work on that one

// consider adding some idea of back pressure
// consider that this implementation might be simpler if I make the idea of a
// partition / topic id a first class citizen. Like I could make the 

// remove the clone trait bound on message by using Arc
// send reference counted pointers to messages through the channels

const BUFFER_SIZE: usize = 100;

pub trait Routable {
    type Key: Eq + Hash + Clone;
    fn key(&self) -> &Self::Key;
}

#[derive(Clone,Debug)]
pub struct BrokerMessage {
    pub connection_id: String, 
    pub payload: String,
}

impl Routable for BrokerMessage {
    type Key = String;
    fn key(&self) -> &String {
        return &self.connection_id;
    }
}

/*
Using the builder pattern for the broker accomplishes two things:
- we can add ergonomic ways to make many configurations in the future
- we can prevent the run method of the broker from being scheduled twice
*/
pub struct BrokerBuilder {
    buffer_size: usize
}

impl BrokerBuilder {
    pub fn buffer_size(mut self, buffer_size: usize) -> Self {
        self.buffer_size = buffer_size;
        self
    }

    pub fn build<M: Routable + Clone>(self) -> Broker<M> {
        Broker{
            topics: Arc::new(Mutex::new(
                HashMap::<M::Key, Sender<M>>::new()
            )),
            buffer_size: self.buffer_size,
        }
    }
}

impl Default for BrokerBuilder {
    fn default() -> Self {
        BrokerBuilder { buffer_size: BUFFER_SIZE }
    }
}

pub struct WrappedReceiver<M: Routable + Clone> {
    topic_id: M::Key,
    // TODO: modify the wrapped receiver so that clients can't clone the receiver inside the wrapped receiver
    receiver: Receiver<M>,
    topics: Arc<Mutex<HashMap<M::Key, Sender<M>>>>,
}

impl<M: Routable + Clone> Drop for WrappedReceiver<M> {
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

impl<M: Routable + Clone> WrappedReceiver<M> {
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


#[derive(Clone)]
pub struct Broker<M: Routable + Clone> {
    // mapping of topic ids to senders that can be used to signal the subscribers to a topic
    topics: Arc<Mutex<HashMap<M::Key, Sender<M>>>>,
    buffer_size: usize,
}

impl<M: Routable + Clone> Broker<M> {
    // method that can be used by a websocket client to subscribe to a topic in the broker
    // pub fn subscribe(&self, topic_id: M::Key) -> Subscription<M> {
        
    // }

    
    pub fn register(&self, topic_id: M::Key) -> (Sender<M>, WrappedReceiver<M>) {
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