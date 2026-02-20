// use axum::routing::connect;
use tokio::sync::mpsc;
use tokio::sync::mpsc::{
    Sender,
    Receiver,
};
use std::sync::{Mutex, Arc};
use std::collections::HashMap;

// create a struct to represent the broker, this will be available 
// as top level state
// create a method on the broker that allow a connection to be registered
// with the broker. This method should return return a channel transmitter 
// that a handler can use to send messages to the broker and a channel 
// receiver that the handler can use to receive messages from the broker

// I might have to leave the broker as some sort of shared state
// that is reference-able by any handler-task
// there is a hybrid approach in which the broker is used as shared state when 
// adding or removing connections but other wise only is accessed via channels

// make a wrapper around the broker that exposes methods to add new connections
// that internally use a mutex

// instead of passing around string literals, pass around either a reference counted
// pointer to a string or an immutable reference to a string. Not sure how the
// lifetimes would work on that one

// parameterize the broker struct such that it takes a message type, a connection
// identifier type, and a filter function that takes a message and a connection
// identifier and returns true if the message should be sent to that connection

// use the type state function to prevent the run function from being called on 
// the broker multiple times. Broker and RunningBroker types
// the same result can also be reached by just starting the broker in the constructor
// this would allow us to return a join handle which we can use to tell if the 
// task has finished / failed or stop the task
// try the broker builder pattern

// consider adding some idea of back pressure
// consider that this implementation might be simpler if I make the idea of a
// partition / topic id a first class citizen. Like I could make the 

const BUFFER_SIZE: usize = 100;

#[derive(Clone,Debug)]
pub struct Message {
    pub connection_id: String, 
    pub payload: String,
}

pub struct Broker {
    /// transmitter that can be cloned so that handlers can send messages
    /// to the broker
    tx: Sender<Message>,
    /// collection of handler connection ids and transmitters that can be 
    /// used to send information to those handlers
    connections: Arc<Mutex<HashMap<String, Sender<Message>>>>
}

impl Broker {
    pub fn new() -> Broker {
        let (tx, mut rx ) = mpsc::channel::<Message>(BUFFER_SIZE);
        let connections = Arc::new(Mutex::new(
            HashMap::<String, Sender<Message>>::new(),
        ));

        // move the receiver into the spawned thread
        let connections_clone = Arc::clone(&connections);
        tokio::spawn(async move {
            loop {
                // TODO: handle the nil case here as that indicates that all the senders have disconnected
                //       we don't want to panic on graceful shutdown
                let message = rx.recv().await.unwrap();
                // cannot hold the lock across calls to await
                // this is because the held lock may not be transferable between threads and the 
                // regular std::sync::mutex is not async safe
                // we should instead gather all the transmitters that we need to send messages 
                // over first so we can release the lock before performing any async operations
                let senders: Vec<Sender<Message>> = {
                    // TODO: I think an error when trying to lock the mutex indicates that another thread
                    //       has panicked while holding the lock. In that case we want this thread to panic too
                    let connections = connections_clone.lock().unwrap();
                    connections
                        .iter()
                        .filter(|elem| -> bool {*elem.0 != message.connection_id})
                        .map(|(_, sender)| sender.clone())
                        .collect()
                };
                for sender in senders {
                    // TODO: an error here indicates that there is no longer a receiver listening on this transmitter
                    //       in that case we should not panic here. Instead we should remove that receiver from the 
                    //       mapping
                    // TODO: an error here may also indicate that a buffer is full. This will happen if the receiver is
                    //       slow to process messages. Consider using try-send to prevent slow receivers from blocking
                    //       sending messages to fast receivers
                    sender.send(message.clone()).await.unwrap();
                }
            }
        });

        Broker { tx, connections }
    }
    /// method that can be used to register a new connection with the 
    /// broker. This takes a connection id and returns a mp transmitter that 
    /// the handler can use to send messages and a sp receiver that the handler
    /// can use to receive messages from the broker
    pub fn register(&self, connection_id: String) -> (Sender<Message>, Receiver<Message>) {
        // TODO: do not let the same connection id register twice. This would result in a dropped connection
        //       for the transmitter and receiver associated with the connection the first time that it is registered
        // clone the transmitter that is used to send messages to the broker
        let tx_broker = self.tx.clone();
        // create a transmitter, receiver pair for sending messages from the broker to the websocket task
        let (tx_client, rx_client) = mpsc::channel(BUFFER_SIZE);
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
    pub fn deregister(&self, connection_id: &str) {
        let mut connections = self.connections.lock().unwrap();
        connections.remove(connection_id);
    }
}