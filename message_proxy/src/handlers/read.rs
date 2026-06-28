use tokio::sync::broadcast::{
    Sender as BCSender, 
};
use tokio::sync::oneshot::{
    Sender
};
use yrs::updates::encoder::Encode;
use std::sync::Arc;
use std::time::Instant;
use std::ops::Range;
use tokio_util::sync::CancellationToken;
use axum::{
    extract::ws::{
        Message, 
        WebSocket,
    },
    Error,
};
use futures_util::{
    StreamExt, 
    stream::{SplitStream},
};
use yrs::{
    StateVector, Update,
    sync::protocol::SyncMessage,
    updates::decoder::Decode,
    DeleteSet,
};
use tracing::{Level, event, instrument};

use crate::repository::{
    Repository,
    ClientDeletionSet,
};
use crate::config::otel::{
    WSMessageType,
};
use crate::handlers::{
    WebsocketHandler, 
    Decoded,
    TaskError,
    ReaderEvent,
    UpdateMessage
};

struct MessageOutcome {
    persisted_update: bool,
    persisted_deletion: bool,
    broadcast_message: bool,
}

impl From<(bool, bool, bool)> for MessageOutcome {
    fn from(value: (bool, bool, bool)) -> Self {
        MessageOutcome {
            persisted_update: value.0,
            persisted_deletion: value.1,
            broadcast_message: value.2,
        }
    }
}

impl <R: Repository> WebsocketHandler<R> {
    #[instrument(skip_all)]
    fn forward_client_sync_step_one(
        sync_sender_opt: &mut Option<Sender<ReaderEvent>>,
        state_vector: StateVector,
        start: Instant,
    ) -> Result<(), TaskError> {
        // sync step one messages get processed by the writer
        // TODO: may need some error handling code here in the case that 
        // the client receiver has already been dropped
        if let Some(sync_sender) = sync_sender_opt.take() {
            sync_sender.send(ReaderEvent::ClientSyncStep1(state_vector.encode_v1(), start))
                .map_err(TaskError::ReaderToWriterSendError)?;
        }
        Ok(())
    }

    async fn decode_helper(
        &self,
        result: Option<Result<Message, Error>>,
        client_id: u64,
    ) -> Decoded {
        /*
        This result can be one of these things:
        - Binary websocket message
            - when we get these we want to decode them into one of a few types of Sync Messages
            - alternatively, if we fail to decode we want to close the connection 
        - Closing frame websocket message
            - when we get these we want to indicate to the writer that the websocket connection is closing
        - Ping, Pong, Text websocket message
            - we are not interested in these
        - Err
            - these indicate that there was an error buffering messages at the OS level
            - when we get these we want to indicate to the writer that the websocket connection is closing
        - None
            - these indicate that the websocket connection has closed without a closing frame
            - when we get these we want to indicate to the writer that the websocket connection is closing
        */
        let start = Instant::now();
        match result {
            Some(Ok(Message::Binary(bytes))) => {
                match SyncMessage::decode_v1(&bytes) {
                    Ok(sync_message) => {
                        let message_type = match sync_message {
                            SyncMessage::SyncStep1(_) => { WSMessageType::SyncStep1 },
                            SyncMessage::SyncStep2(_) => {WSMessageType::SyncStep2 },
                            SyncMessage::Update(_) => { WSMessageType::Update },
                        };
                        self.metrics_ws.record_received_ws_message(bytes.len(), message_type);
                        Decoded::Valid(sync_message, start)
                    },
                    Err(e) => {
                        self.metrics_ws.record_received_ws_message(
                            bytes.len(), WSMessageType::Error
                        );
                        event!(Level::WARN, client_id_src=client_id, "failed to decode message with error: {e}");
                        Decoded::Skip
                    },
                }
            },
            Some(Ok(Message::Close(_))) | None | Some(Err(_)) => {
                Decoded::Failure
            },
            Some(Ok(_)) => Decoded::Skip,
        }
    }

    fn parse_message(
        &self,
        encoded_update: &Vec<u8>,
    ) -> Result<(Option<u32>, Option<ClientDeletionSet>), TaskError> {
        // decoded Updates cannot be held across await boundaries because it is not send
        // when possible, we need to drop the update before an await boundary
        let decoded_update = Update::decode_v1(&encoded_update)?;
        event!(
            Level::DEBUG,
            client_id_src=self.client_id,
            insertions=?&decoded_update.insertions(false),
            "received update information"
        );
        // Updates are zero indexed, however the state vector
        // holds the exclusive upper bound of operations received. Meaning that the state vector
        // stores the offset of the next operation a client expects to receive.
        // Find the inclusive upper bound of operation offsets contained by this update
        // for the src client id associated with this connection
        let insertions = decoded_update.insertions(false);
        let new_offset: Option<u32> = match insertions.get(&self.client_id) {
            Some(id_range) => {
                // this gives us an iterator over the ranges of operations in the id range
                // this is because the id range can be disjoint instead of continuous
                let new_offset = id_range.iter()
                    // this gives us the last range in the iterator of ranges
                    .last()
                    // this gives us the exclusive upper bound of the last range 
                    .map(|r| r.end)
                    // this gives us the inclusive upper bound of the last range
                    .map(|o| o - 1);
                new_offset
            },
            // this is the case where there are no operations in the update for the current client_id
            None => None
        };

        let deletions: &DeleteSet = decoded_update.delete_set();
        // DeleteSet does not expose a get method for accessIng the underlying IdSet so 
        // I have to use this iter().find() access method

        // iter iterates over references to key value pairs in the IdSet hashmap
        // find takes reference to the elements that we are iterating over
        // for this reason k and v are both && (double references)
        // we can use the &k pattern to destructure the &&element into a u64 and 
        // &&IdRange because u64 implements the copy trait. We do not have to take
        // ownership of the k symbol
        let client_deletion_set: Option<Vec<Range<u32>>> = deletions.iter()
            // find takes a mutable reference to the element
            .find(|(k, _)| **k == self.client_id)
            // map takes the element itself
            .map(|(_, id_range)| {
                // iterate over ranges in the IdRange
                id_range.iter().cloned().collect()
            });

        Ok((new_offset, client_deletion_set))
    }

    #[instrument(skip_all)]
    async fn persist_and_broadcast_update(
        &self,
        encoded_update: Vec<u8>,
        broker_sender: &BCSender<UpdateMessage>,
    ) -> Result<(Option<u32>, MessageOutcome), TaskError> {
        let (mut persisted_update, mut persisted_deletion) = (false, false);
        // parse the optional offset of the update from the message
        // parse the optional deletion set from the message
        let (new_offset, new_delete_set) = self.parse_message(&encoded_update)?;
        if let Some(new_offset) = new_offset {
            // read the previously received offset for this client_id
            let last_received_offset = self.repo.read_last_received_offset(
                self.topic_id, self.client_id
            ).await?;
            // compare the offset to the previously received last offset for this client
            // Drop the update if the offset of the update is less than or equal 
            // to the offset of the most recent update from this client
            // Also, when we receive a client sync step two message, we need to throw it out 
            // if it has no operations in it because we don't want to save an empty update
            // at a offset that a valid update would be saved at
            if last_received_offset.is_none_or(|o| o < new_offset) {
                // insert the update into the operations table if this message has updates in
                // it and the offset of the update is greater than the previous updates offset
                // saving wether or not the message was persisted
                self.repo.write_operation(
                    self.topic_id, self.user_id, self.client_id, 
                    new_offset, &encoded_update,
                ).await?;
                persisted_update = true;
            } else {
                event!(
                    Level::DEBUG,
                    client_id_src=self.client_id,
                    "skipping persisting this operation because the old offset ({:?}) and new offset ({}) are >=",
                    last_received_offset,
                    new_offset,
                )
            }
        }
        // if this message has deletions in it
        if let Some(ref deletion_set) = new_delete_set {
            // attempt to write the deletions to the deletions table, saving wether or not 
            // the deletions were persisted
            persisted_deletion = self.repo.write_deletion_set_if_novel(
                self.topic_id, self.user_id, self.client_id, deletion_set
            ).await?;
        }
        // broadcast the message if it had either novel updates or novel deletions
        if persisted_update || persisted_deletion {
            let update_message = UpdateMessage {
                client_id: self.client_id,
                offset: None,
                payload: Arc::new(encoded_update),
                has_deletion: !new_delete_set.is_none(),
            };
            broker_sender.send(update_message)?;
        }

        Ok((
            new_offset,
            MessageOutcome::from((
                persisted_update, 
                persisted_deletion, 
                persisted_update || persisted_deletion,
            )),
        ))
    }

    #[instrument(skip_all)]
    async fn process_client_sync_step_two(
        &self,
        encoded_update: Vec<u8>,
        broker_sender: &BCSender<UpdateMessage>,
        start: Instant,
    ) -> Result<Option<u32>, TaskError> {
        let update_size_bytes = encoded_update.len();
        let (new_offset, message_outcome) = self.persist_and_broadcast_update(
            encoded_update, broker_sender,
        ).await?;
        event!(
            name: "reader_client_sync_step_two_canonical_log_line",
            Level::INFO,
            new_offset,
            duration_ns=start.elapsed().as_nanos(),
            skipped_write_update=message_outcome.persisted_update,
            skipped_write_delete=message_outcome.persisted_deletion,
            skipped_broadcast=message_outcome.broadcast_message,
            update_size_bytes,
            topic_id=self.topic_id.as_hyphenated().to_string(),
            user_id=self.user_id.as_hyphenated().to_string(),
            client_id_src=self.client_id,
            "received an persisted client sync step two message, transitioning from handshake to hot path for reader",
        );
        return Ok(new_offset);
    }

    #[instrument(skip_all)]
    async fn reader_hot_path(
        &self,
        encoded_update: Vec<u8>,
        sender_broker: &BCSender<UpdateMessage>,
        start: Instant,
    ) -> Result<Option<u32>, TaskError> {
        let update_size_bytes = encoded_update.len();
        let (new_offset, message_outcome) = self.persist_and_broadcast_update(
            encoded_update, sender_broker
        ).await?;
        event!(
            name: "reader_hot_path_canonical_log_line",
            Level::INFO,
            new_offset,
            skipped_write_update=message_outcome.persisted_update,
            skipped_write_delete=message_outcome.persisted_deletion,
            skipped_broadcast=message_outcome.broadcast_message,
            update_size_bytes,
            duration_ns = start.elapsed().as_nanos(),
            topic_id=self.topic_id.as_hyphenated().to_string(),
            user_id=self.user_id.as_hyphenated().to_string(),
            client_id_src=self.client_id,
            "reader_hot_path_canonical_log_line",
        );
        return Ok(new_offset);
    }


    pub async fn read(
        &self,
        mut websocket_receiver: SplitStream<WebSocket>,
        broker_sender: BCSender<UpdateMessage>,
        sync_sender: Sender<ReaderEvent>,
        cancel_token: CancellationToken,
    ) -> Result<(), TaskError> {
        let mut sync_sender_opt = Some(sync_sender);
        // expect the client to send us a sync step one message including their version vector
        // or a sync step two message including the local updates that the server does not yet have
        let mut _last_received_offset = loop {
            tokio::select! {
                biased;
                _ = cancel_token.cancelled() => {
                    return Ok(());
                },
                result = websocket_receiver.next() => match self.decode_helper(result, self.client_id).await {
                    Decoded::Valid(SyncMessage::SyncStep1(sv), start) => {
                        let _ = Self::forward_client_sync_step_one(&mut sync_sender_opt, sv, start);
                    },
                    Decoded::Valid(SyncMessage::SyncStep2(encoded_bulk_update), start) => {
                        // if it is a sync step two message, process it at the reader then break    
                        break self.process_client_sync_step_two(
                            encoded_bulk_update,
                            &broker_sender,
                            start,
                        ).await?;
                    },
                    // if it is an update message, skip it
                    Decoded::Valid(SyncMessage::Update(_), _) => {},
                    Decoded::Skip => {},
                    Decoded::Failure => { 
                        return Ok(()); 
                    }
                },
            }
        };
        event!(Level::DEBUG, client_id_src=self.client_id, "entering reader hot path");
        // enter the hot path
        loop {
            tokio::select! {
                biased;
                _ = cancel_token.cancelled() => {
                    event!(Level::DEBUG, client_id_src=self.client_id, "returning from reader because of cancellation token");
                    return Ok(());
                },
                result = websocket_receiver.next() => match self.decode_helper(result, self.client_id).await {
                    Decoded::Valid(SyncMessage::SyncStep1(sv), start) => {
                        let _ = Self::forward_client_sync_step_one(&mut sync_sender_opt, sv, start);
                    },
                    Decoded::Valid(SyncMessage::SyncStep2(_), _) => { 
                        /* if it is a sync step two message, ignore it. 
                        We must have already processed the client sync 
                        step two message in order to get to the reader hot path */ 
                    },
                    // if it is an update message, skip it
                    Decoded::Valid(SyncMessage::Update(encoded_update), start) => {
                        _last_received_offset = self.reader_hot_path(
                            encoded_update, &broker_sender, start,
                        ).await?
                    },
                    Decoded::Skip => {},
                    Decoded::Failure => { return Ok(()); }
                },
            }
        }
    }
}