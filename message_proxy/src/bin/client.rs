use std::env;
use std::str;

use futures_util::{StreamExt, future, pin_mut};
use tokio::io::{AsyncReadExt, AsyncWriteExt};
use tokio_tungstenite::tungstenite;
use tokio_tungstenite::{connect_async, tungstenite::protocol::Message};

use yrs::GetString;
use yrs::updates::decoder::Decode;
use yrs::{Doc, Transact, Text, Update};


/*
Modifications to make:
- [x] maintain an internal document state
- [ ] read modifications from std in and apply them to the document
- [ ] send all local modifications over the websocket
- [ ] on each received message, print the document state to stdout
- ignore standard in
*/

#[tokio::main]
async fn main() {
    // create a yjs doc
    let doc = Doc::new();
    let text = doc.get_or_insert_text("test");
    // read the websocket url from the os env
    let url = env::args().nth(1).unwrap_or_else(
        || panic!("this program requires at least one argument")
    );
    // spawn a reader task that reads from stdin and writes to a channel 
    let (stdin_tx, stdin_rx) = futures_channel::mpsc::unbounded();
    tokio::spawn(read_stdin(stdin_tx));
    // create the websocket connection
    let (ws_stream, _) = connect_async(&url).await.expect("Failed to connect");
    println!("WebSocket handshake has been successfully completed");
    // split the websocket connection into a reader and writer stream 
    let (write, read) = ws_stream.split();
    // for each input to stdin, apply the input to the head of the text 
    // then send the corresponding update over the websocket
    let stdin_to_doc_to_ws = stdin_rx.map(|message| -> Result<Message, tungstenite::Error>  {
        match message {
            Message::Binary(contents) => {
                let contents = str::from_utf8(&contents)
                    .unwrap_or("error parsing")
                    .replace('\n', "")
                    .replace('\r', "");
                let mut txn = doc.transact_mut();
                text.push(&mut txn, &contents);
                // get the update associated with this operation then write it to the websocket
                let update = txn.encode_update_v1();
                Ok(Message::Binary(update.into()))
            }
            _ => Ok(Message::Text("howdy".into()))
        }
    }).forward(write);
    // for each message received over the websocket connection, apply that modification to
    // the document, then print the resolved document
    let ws_to_stdout = {
        // TODO: each websocket message should be a yrs binary update message
        // apply the binary update message to the document then print out the document
        // representation
        read.for_each(|message| async {
            let data = message.unwrap().into_data();
            // parse the incoming bytes into a yrs Update
            if let Ok(update) = Update::decode_v1(&data) {
                let mut txn = doc.transact_mut();
                txn.apply_update(update).unwrap();
                let repr = text.get_string(&txn);
                let view = format!("===\n{}\n===\n", repr);
                tokio::io::stdout().write_all(view.as_bytes()).await.unwrap();
            }
        })
    };

    pin_mut!(stdin_to_doc_to_ws, ws_to_stdout);
    // wait for one of the two futures to complete. This means we are waiting for either the 
    // stdin stream to run out of inputs or the ws reader to close
    future::select(stdin_to_doc_to_ws, ws_to_stdout).await;
}

// Our helper method which will read data from stdin and send it along the
// sender provided.
async fn read_stdin(tx: futures_channel::mpsc::UnboundedSender<Message>) {
    let mut stdin = tokio::io::stdin();
    loop {
        let mut buf = vec![0; 1024];
        let n = match stdin.read(&mut buf).await {
            Err(_) | Ok(0) => break,
            Ok(n) => n,
        };
        buf.truncate(n);
        tx.unbounded_send(Message::binary(buf)).unwrap();
    }
}