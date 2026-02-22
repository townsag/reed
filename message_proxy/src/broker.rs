use std::clone::Clone;
use std::sync::atomic::{AtomicUsize, Ordering};
use tokio::sync::mpsc;
use tokio::sync::mpsc::{
    Sender,
    Receiver,
};
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
    type Key: Eq + Hash;
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

    pub fn build<M: Routable + Clone>(self) -> (Broker<M>, impl Future<Output = ()>) {
        // create the channel that clients can use to send messages to to the broker
        let (tx, rx) = mpsc::channel::<M>(self.buffer_size);
        // create the connections hashmap that holds a mapping between the connection id
        // and the channel that is used to pass messages to that connection
        let connections = Arc::new(Mutex::new(
            HashMap::<M::Key, Sender<M>>::new(),
        ));
        let connections_clone = Arc::clone(&connections);
        (Broker{ tx, connections, buffer_size: self.buffer_size }, run_broker(rx, connections_clone))
    }
}

impl Default for BrokerBuilder {
    fn default() -> Self {
        BrokerBuilder { buffer_size: BUFFER_SIZE }
    }
}

async fn run_broker<M: Routable + Clone>(
    mut rx: Receiver<M>, 
    connections: Arc<Mutex<HashMap<M::Key, Sender<M>>>>,
) {
    loop {
        // TODO: handle the nil case here as that indicates that all the senders have disconnected
        //       we don't want to panic on graceful shutdown
        let message = rx.recv().await.unwrap();
        // cannot hold the lock across calls to await
        // this is because the held lock may not be transferable between threads and the 
        // regular std::sync::mutex is not async safe
        // we should instead gather all the transmitters that we need to send messages 
        // over first so we can release the lock before performing any async operations
        let senders: Vec<Sender<M>> = {
            // TODO: I think an error when trying to lock the mutex indicates that another thread
            //       has panicked while holding the lock. In that case we want this thread to panic too
            let connections = connections.lock().unwrap();
            connections
                .iter()
                .filter(|(id, _)| *id != message.key())
                .map(|(_, sender)| sender.clone())
                .collect()
        };
        // we clone the senders here so that we do not have to hold the connections mutex while 
        // sending messages over channels. The clones of the channel transmitters will go out 
        // of scope at the end of this loop
        for sender in senders {
            // TODO: an error here indicates that there is no longer a receiver listening on this transmitter
            //       in that case we should not panic here. Instead we should remove that receiver from the 
            //       mapping
            // TODO: an error here may also indicate that a buffer is full. This will happen if the receiver is
            //       slow to process messages. Consider using try-send to prevent slow receivers from blocking
            //       sending messages to fast receivers
            // TODO: remove this clone by creating a reference counted pointer and then passing the reference 
            //       counted pointer down the channel
            sender.send(message.clone()).await.unwrap();
        }
    }
}

#[derive(Clone)]
pub struct Broker<M: Routable + Clone> {
    /// transmitter that can be cloned so that handlers can send messages
    /// to the broker
    tx: Sender<M>,
    /// collection of handler connection ids and transmitters that can be 
    /// used to send information to those handlers
    connections: Arc<Mutex<HashMap<M::Key, Sender<M>>>>,
    buffer_size: usize,
}

impl<M: Routable + Clone> Broker<M> {
    /// method that can be used to register a new connection with the 
    /// broker. This takes a connection id and returns a mp transmitter that 
    /// the handler can use to send messages and a sp receiver that the handler
    /// can use to receive messages from the broker
    pub fn register(&self, connection_id: M::Key) -> (Sender<M>, Receiver<M>) {
        // TODO: do not let the same connection id register twice. This would result in a dropped connection
        //       for the transmitter and receiver associated with the connection the first time that it is registered
        // clone the transmitter that is used to send messages to the broker
        let tx_broker = self.tx.clone();
        // create a transmitter, receiver pair for sending messages from the broker to the websocket task
        let (tx_client, rx_client) = mpsc::channel(self.buffer_size);
        {
            // store the transmitter associated with sending messages to this client
            let mut connections = self.connections.lock().unwrap();
            connections.insert(connection_id, tx_client);
        }

        // return the transmitter used to send messages to the broker and the receiver used to
        // get messages from the broker
        (tx_broker, rx_client)
    }
    /// method that can be used to deregister a connection with the broker
    /// this takes a connection id and returns nothing
    pub fn deregister(&self, connection_id: &M::Key) {
        let mut connections = self.connections.lock().unwrap();
        connections.remove(connection_id);
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