use clap::Parser;
use anyhow::{
    Context, Result, anyhow
};
use futures_util::{SinkExt, StreamExt};
use uuid::Uuid;
use tokio_util::sync::CancellationToken;
use tokio::{
    task::JoinSet,
    sync::mpsc::{
        self, Receiver, Sender
    },
    time::{self, Duration, Instant}
};
use tokio_tungstenite::{
    connect_async,
    tungstenite::protocol::Message,
};
use rand_distr::{
    Distribution, Exp,
};
use rand::{
    self,
    // this is a rng implementation that is Send and 
    // is statistically random (not cryptographically random)
    rngs::SmallRng,
};
use yrs::{
    Doc, GetString, IndexedSequence, Options, ReadTxn, StickyIndex, Text, TextRef, Transact, Update, sync::SyncMessage, updates::{
        decoder::Decode, encoder::Encode
    }
};


#[derive(Parser, Debug)]
#[command(name = "mpbench")]
#[command(about = "load test the message proxy service by creating a number of clients")]
struct Config {
    #[arg(short = 'H', long = "hostname")]
    mp_service_hostname: String,
    /// number of clients to be created 
    #[arg(short = 'c', long = "num-clients", default_value_t = 1)]
    num_clients: i32,
    /// how many documents should these clients be distributed across
    #[arg(short = 'd', long = "num-documents", default_value_t = 1)]
    num_documents: i32,
    /// how many edits per minute should each client make
    #[arg(short = 'o', long = "operations-per-minute", default_value_t = 200)]
    operations_per_minute: i32,
    /// how many times per minute should each client disconnect then reconnect
    #[arg(short = 'r', long = "reconnections-per-minute", default_value_t = 2.0)]
    reconnections_per_minute: f32,
    /// how many seconds should the test run
    #[arg(short = 'l', long = "length", default_value_t = 60)]
    length_seconds: i32,
}

enum LoadEvent {
    Reconnection,
    Operation,
}

async fn generate_events(
    operations_per_minute: i32,
    reconnections_per_minute: f32,
    event_sender: Sender<LoadEvent>,
    cancel: CancellationToken,
) -> Result<u32> {
    let mut count_ops = 0;
    let mut rng: SmallRng = rand::make_rng();

    let ops_exp_dist = Exp::new(operations_per_minute as f32).unwrap();
    let recon_exp_dist = Exp::new(reconnections_per_minute).unwrap();
    // convert from fractional minutes represented by f32 to integer milliseconds by u64
    let mut next_op_offset = (ops_exp_dist.sample(&mut rng) * 60_000.0) as u64;
    let mut next_op_time = Instant::now() + Duration::from_millis(next_op_offset);
    println!("next op time offset: {}", next_op_offset);
    
    
    let mut next_recon_offset = (recon_exp_dist.sample(&mut rng) * 60_000.0) as u64;
    let mut next_recon_time = Instant::now() + Duration::from_millis(next_recon_offset);
    println!("next recon time offset: {}", next_recon_offset);
    println!("current time: {:?}", Instant::now());
    println!("time of next recon: {:?}", next_recon_time);

    loop {
        tokio::select! {
            biased;
            _ = cancel.cancelled() => {
                return Ok(count_ops);
            }
            _ = time::sleep_until(next_recon_time) => {
                eprintln!("publishing a reconnect event");
                event_sender.send(LoadEvent::Reconnection).await?;
                next_recon_offset = (recon_exp_dist.sample(&mut rng) * 60_000.0) as u64;
                next_recon_time = next_recon_time + Duration::from_millis(next_recon_offset);
            }
            _ = time::sleep_until(next_op_time) => {
                eprintln!("publishing operation event, {:?}", Instant::now());
                count_ops += 1;
                event_sender.send(LoadEvent::Operation).await?;
                next_op_offset = (ops_exp_dist.sample(&mut rng) * 60_000.0) as u64;
                // anchor the next operation event time to the time that the current event should
                // have fired, this way the time to publish the event or get scheduled does not
                // skew the distribution of operation times
                next_op_time = next_op_time + Duration::from_millis(next_op_offset);
            },
        }
    }
}

struct DocumentState {
    doc: Doc,
    text: TextRef,
    // Sticky index holds the position after where we have most recently inserted
    sticky_index: StickyIndex,
    received_server_sync_step_two: bool,
    sent_client_sync_step_two: bool,
    count_local_operations: u32,
}

const EXAMPLE_TEXT: &str = "In a hole in the ground there lived a hobbit. Not a nasty,
dirty, wet hole, filled with the ends of worms and an oozy
smell, nor yet a dry, bare, sandy hole with nothing in it to
sit down on or to eat: it was a hobbit-hole, and that means
comfort.";

impl DocumentState {
    fn new(client_id: u64) -> Result<Self> {
        let doc = Doc::with_options(Options::with_client_id(client_id));
        let text = doc.get_or_insert_text("text");
        let txn = doc.transact();
        let sticky_index = text
            .sticky_index(&txn, 0, yrs::Assoc::Before)
            .ok_or(anyhow!("failed to create a sticky index upon startup"))?;
        drop(txn);
        Ok(DocumentState { 
            doc: doc,
            text: text,
            sticky_index: sticky_index,
            received_server_sync_step_two: false,
            sent_client_sync_step_two: false,
            count_local_operations: 0,
        })
    }
    fn reset_handshake_state(&mut self) {
        self.received_server_sync_step_two = false;
        self.sent_client_sync_step_two = false;
    }
    fn ready_for_remote_update(&self) -> bool {
        return self.received_server_sync_step_two;
    }
    fn ready_for_local_update(&self) -> bool {
        return self.sent_client_sync_step_two;
    }
    fn make_client_sync_step_one(&self) -> SyncMessage {
        let sv = self.doc.transact().state_vector();
        SyncMessage::SyncStep1(sv)
    }
    fn receive_sync_message(&mut self, message: SyncMessage) -> Result<Option<SyncMessage>> {
        match message {
            SyncMessage::SyncStep1(remote_sv) => {
                eprintln!("received remote state vector: {:?}", remote_sv);
                // make a state vector that represents the local document state
                let mut sv = self.doc.transact().state_vector();
                eprintln!("have local state vector: {:?}", sv);
                // doctor the state vector so the offset of the current client_id is the same for the 
                // remote state vector
                let local_client_id = self.doc.client_id();
                sv.set_min(local_client_id, remote_sv.get(&local_client_id));
                eprintln!("local state vector after patch: {:?}", sv);
                // create a diff containing the operations not included in the doctored state vector
                // this includes all the operations the remote server has not seen
                let update = self.doc.transact().encode_diff_v1(&sv);
                {
                    let decoded_update = Update::decode_v1(&update)?;
                    eprintln!(
                        "sending update as sync step two: {:?}", 
                        decoded_update,
                    );
                }
                // send the update over the websocket as a sync step 2 message
                let client_sync_step_2 = SyncMessage::SyncStep2(update);
                self.sent_client_sync_step_two = true;
                return Ok(Some(client_sync_step_2));
            },
            SyncMessage::SyncStep2(encoded_update) => {
                let decoded_update = Update::decode_v1(&encoded_update)?;
                // apply the sync step 2 message to local state like an update
                self.doc.transact_mut().apply_update(decoded_update)?;
                self.received_server_sync_step_two = true;
                return Ok(None);
            },
            SyncMessage::Update(encoded_update) => {
                // ignore update messages before we have received a server
                // sync step two message
                if !self.ready_for_remote_update() {
                    return Ok(None);
                }
                let decoded_update = Update::decode_v1(&encoded_update)?;
                self.doc.transact_mut().apply_update(decoded_update)?;
                return Ok(None);
            },
        }
    }
    fn receive_operation_event(&mut self) -> Result<SyncMessage> {
        // insert the next character in the hobbit intro into the text at the stick index
        let next_offset = self.count_local_operations as usize % EXAMPLE_TEXT.len();
        self.count_local_operations += 1;
        let next_characters = &EXAMPLE_TEXT[next_offset..next_offset + 1];

        // create a update sync message resulting from this insert and return it
        let mut txn = self.doc.transact_mut();
        let offset = self.sticky_index
            .get_offset(&txn)
            .ok_or(anyhow!("failed to insert a new chunk because the computed index was out of range"))?;
        self.text.insert(&mut txn, offset.index, next_characters);
        let encoded_update = txn.encode_update_v1();

        // update the sticky index to reflect the recent change
        self.sticky_index = self.text
            .sticky_index(&txn, offset.index + next_characters.len() as u32, yrs::Assoc::Before)
            .ok_or(anyhow!("failed to create new index because the computed index was out of range"))?;

        Ok(SyncMessage::Update(encoded_update))
    }
    fn get_representation(&self) -> String {
        let txn = &self.doc.transact();
        self.text.get_string(txn)
    }
    fn get_count_local_ops(&self) -> u32 {
        self.count_local_operations
    }
}

async fn process_events(
    config: ClientConfig,
    mut event_receiver: Receiver<LoadEvent>,
    cancel: CancellationToken,
) -> Result<u32> {
    /*
    Insight:
    - in the case of the message proxy server, we have to wait for the client to send the sync 
      step two message before we start listening for client update messages
    - In order to avoid lost updates we should refrain from processing operation events before we
      have send the client sync step two message
     */
    // create document state
    let mut state = DocumentState::new(config.client_id)?;
    loop {
        // make a connection to the server
        eprintln!("connecting to websocket server at: {}", config.dump_url());
        let (ws_stream, _) = connect_async(config.dump_url()).await
            .expect("unable to connect to websocket server");
        let (mut write, mut read) = ws_stream.split();
        eprintln!("successfully connected to websocket server");
        let client_sync_step_one = state.make_client_sync_step_one();
        write.send(Message::Binary(client_sync_step_one.encode_v1().into())).await?;
        eprintln!("sent client sync step one");
        loop {
            tokio::select! {
                ws_result = read.next() => match ws_result {
                    Some(std::result::Result::Ok(message)) => {
                        match SyncMessage::decode_v1(&message.into_data()) {
                            Ok(sync_message) => {
                                if let Some(client_sync_step_2) = state.receive_sync_message(sync_message)? {
                                    write.send(Message::Binary(client_sync_step_2.encode_v1().into())).await?;
                                }
                            },
                            Err(e) => {
                                return Err(e).with_context(|| "failed to read from websocket connection");
                            },
                        }
                    },
                    Some(Err(e)) => { return Err(e).with_context(|| "failed to read from websocket connection") },
                    None => {
                        return Err(anyhow!("received none from the websocket reader, indicating the connection is closed"));
                    },
                },
                event = event_receiver.recv(), if state.ready_for_local_update() => match event {
                    Some(LoadEvent::Operation) => {
                        // on operation event, apply to document state and send websocket message
                        let update_sync_message = state.receive_operation_event()?;
                        write.send(Message::Binary(update_sync_message.encode_v1().into())).await?;
                    },
                    Some(LoadEvent::Reconnection) => {
                        // on reconnect event, break and restart loop
                        state.reset_handshake_state();
                        break;
                    },
                    None => {
                        eprintln!("text representation: {}", state.get_representation());
                        return Err(anyhow!("received none from event channel, indicating event spawning task panicked"));
                    },
                },
                _ = cancel.cancelled() => {
                    eprintln!("text representation: {}", state.get_representation());
                    return Ok(());
                },
            }
        }
    };
}

struct ClientConfig {
    hostname: String,
    document_id: Uuid,
    user_id: Uuid,
    client_id: u64,
    operations_per_minute: i32,
    reconnections_per_minute: f32,
}

impl ClientConfig {
    fn dump_url(&self) -> String {
        format!(
            "ws://{}/ws/{}/{}?client_id={}", 
            self.hostname,
            self.document_id.as_hyphenated(), 
            self.user_id.as_hyphenated(),
            self.client_id.to_string(),
        )
    }
}

async fn pseudo_client(
    config: ClientConfig,
    cancel: CancellationToken,
) -> Result<()> {
    // spawn a task that will generate events
    let (event_sender, event_receiver) = mpsc::channel(100);
    let mut set = JoinSet::new();
    set.spawn(generate_events(
        config.operations_per_minute, config.reconnections_per_minute, event_sender, cancel.clone(),
    ));
    // spawn a task that will process events, generate messages, and process websocket messages
    // this second task should own the websocket connection, the document state, and the receiver
    // for the channel the first task published events to
    set.spawn(process_events(config, event_receiver, cancel));
    while let Some(result) = set.join_next().await {
        eprintln!("result: {:?}", result);
    }

    Ok(())
}

/*
I will be making many tasks that themselves manage tasks. Should I use cooperative cancellation
via cancellation tokens or should I use a join set in the main function and join handles in the
pseudo_client function. This is a forceful cancellation. I think in the future I will want to
perform various clean up tasks, this will be easier with the cancellation token. Furthermore,
the select-loop structure lends itself well to the cancel token approach
*/

#[tokio::main]
async fn main() -> Result<()> {
    let args = Config::parse();
    let finish_at = Instant::now() + Duration::from_secs(args.length_seconds as u64);
    let cancel = CancellationToken::new();
    let documents: Vec<Uuid> = (0..args.num_documents).map(|_| Uuid::new_v4()).collect();
    println!("found args: {:?}", args);
    // spawn n pseudo clients
    // create a config for each pseudo client, distributing clients across the documents randomly
    let mut set = JoinSet::new();
    for i in 0..args.num_clients {
        let config = ClientConfig {
            hostname: args.mp_service_hostname.clone(),
            document_id: documents[i as usize % documents.len()],
            user_id: Uuid::new_v4(),
            client_id: i as u64,
            operations_per_minute: args.operations_per_minute,
            reconnections_per_minute: args.reconnections_per_minute,
        };
        println!("spawning client: {}", i);
        set.spawn(pseudo_client(config, cancel.clone()));
    }
    // use a timer to kill the pseudo clients after length seconds has been reached
    time::sleep_until(finish_at).await;
    cancel.cancel();
    set.join_all().await;
    eprintln!("end time: {:?}", Instant::now());
    Ok(())
}