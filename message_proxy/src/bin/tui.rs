use crossterm::event::{DisableMouseCapture, EnableMouseCapture, EventStream};
use crossterm::terminal::{
    disable_raw_mode, enable_raw_mode, EnterAlternateScreen, LeaveAlternateScreen,
};
use ratatui::backend::CrosstermBackend;
use ratatui::layout::{Constraint, Direction, Layout};
use ratatui::widgets::{Block, Borders, Paragraph};
use ratatui::{Terminal};
use yrs::updates::encoder::Encode;
use yrs::{IndexedSequence, Options, ReadTxn};
use core::panic;
use std::{io, process};
use std::env;
use std::str::FromStr;
use tui_textarea::{Input, Key, TextArea, CursorMove};

use yrs::{
    Doc, GetString, Text, TextRef, Transact, Update, StateVector,
    updates::decoder::Decode,
    sync::protocol::{SyncMessage},
};
use tokio_tungstenite::{connect_async, tungstenite::protocol::Message};
use futures_util::{SinkExt, StreamExt};
use uuid::Uuid;

struct EditorState<'a> {
    pub textarea: TextArea<'a>,
    doc: Doc,
    text: TextRef,
}

struct Config {
    hostname: String,
    topic_id: Uuid,
    user_id: Uuid,
    client_id: u64,
}

impl Config {
    fn build(args: &[String]) -> Result<Config, &'static str> {
        if args.len() < 5 {
            return Err("not enough arguments were provided");
        }

        let hostname = args[1].clone();
        let topic_id_raw = &args[2];
        let user_id_raw = &args[3];
        let client_id_raw = &args[4];
        // println!("topic_id: {}", topic_id_raw);
        let Ok(topic_id) = uuid::Uuid::from_str(&topic_id_raw) else {
            return Err("topic_id was not a valid uuid");
        };
        let Ok(user_id) = uuid::Uuid::from_str(&user_id_raw) else {
            return Err("user_id was not a valid uuid");
        };
        let Ok(client_id) = client_id_raw.parse::<u64>() else {
            return Err("client_id was not a valid u64");
        };
        Ok(Config { hostname, topic_id, user_id, client_id })
    }
    fn dump_url(&self) -> String {
        format!(
            "ws://{}/ws/{}/{}?client_id={}", 
            self.hostname,
            self.topic_id.as_hyphenated(), 
            self.user_id.as_hyphenated(),
            self.client_id.to_string(),
        )
    }
}

impl EditorState<'_> {
    fn new(client_id: Option<u64>) -> (Self, SyncMessage) {
        let options = match client_id {
            Some(client_id) => Options::with_client_id(client_id),
            None => Options::default(),
        };
        let doc = Doc::with_options(options);
        let text = doc.get_or_insert_text("text");
        let mut textarea = TextArea::default();
        textarea.set_block(
            Block::default()
                .borders(Borders::ALL)
                .title("Crossterm Minimal Example"),
        );
        // construct a sync step one message that can be sent over the websocket
        let sv = doc.transact().state_vector();
        return (EditorState { textarea, doc, text }, SyncMessage::SyncStep1(sv))
    }
    // fn text_area_replace(&mut self, new_text: &str) {
    //     let lines = new_text.lines().map(String::from).collect();
    //     self.textarea = TextArea::new(lines);
    // }
    fn cursor_to_offset(&self, row: usize, col: usize, lines: &[&str]) -> usize {
        let mut offset: usize = 0;
        // add one to the offset for each row because the newlines are included
        // in the offset when indexing into the YDoc but not in the count of
        // characters in the tui text area
        offset += row;
        for i in 0..row {
            offset += lines[i].len();
        }
        offset + col
    }
    fn offset_to_cursor(&self, offset: usize, lines: &[&str]) -> (usize, usize) {
        // count our way forward through the lines until we find the line that the cursor
        // should be on. Find the col of the cursor on that line
        // remember that the offset into our text includes newlines but the array of 
        // lines does not include new lines. However there is one more index in a line 
        // than there are characters in that line (not including the newline, which is
        // implicitly included because it is not part of lines)
        // ex: offset 3 ab\ncd is the same as (1,0) in [[a,b], [^c,d]]
        // think about the desired behavior, we cannot have an offset after the 
        // implicit newline at the end of each line
        let (mut row , mut col): (usize, usize) = (0, 0);
        let mut consumed_characters: usize = 0;
        for line in lines {
            // this needs to be <= instead of just < because
            // take the example ab^cd where we have offset: 2 and row: 0, col: 2
            // if we insert a newline here we need to end up with offset: 3 and row: 1, col: 0 
            // for the first line we have line.len() = 2 and consumed_character = 0
            // we and to consume this line and increment row to the next row
            // line.len() + 1 + consumed_characters == 2 + 1 + 0
            // 2 + 1 + 0 !< 3 , however: 2 + 1 + 0 <= 3 
            if line.len() + 1 + consumed_characters <= offset {
                row += 1;
                consumed_characters += line.len() + 1;
            } else {
                col = offset - consumed_characters;
                break
            }
        }
        (row, col)
    }
    fn process_input(&mut self, input: &str) -> SyncMessage {
        // txn holds an mutable borrow to self internally because we don't want to modify the
        // document other than with the txn
        // get the location of the cursor from the text area in terms of the YText
        let offset = self.get_offset();
        // apply the operation to the YText
        let mut txn = self.doc.transact_mut();
        self.text.insert(&mut txn, offset as u32, input);
        let encoded_update = txn.encode_update_v1();
        // update the text area lines to have the new content based on the YText representation
        let status_message = self.text.get_string(&txn);
        self.textarea = TextArea::new(
            status_message.lines().map(String::from).collect()
        );
        drop(txn);
        self.update_cursor(offset + (input.len() as u32));
        
        SyncMessage::Update(encoded_update)
    }
    // receive a message from the server that indicates which operations the client 
    // has already sent to the server. Respond to this message with all the updates that
    // have a happens-after relationship with the servers state vector
    fn process_remote_sync_step_1(&self, remote_state_vector: StateVector) -> SyncMessage {
        // find the updates that have been made by this client specifically in the local 
        // document state that have a happens after relationship with the state vector 
        // sent by the server
        // this is a bit hacky, get a state vector for the local state then use in place 
        // updates to make it as if that state vector had the operation offset that the
        // server has for this client
        // TODO: we should be skipping this step if there are no local operations to encode
        let mut sv = self.doc.transact().state_vector();
        let local_client_id = self.doc.client_id();
        let remote_offset = remote_state_vector.get(&local_client_id);
        // TODO: we may have to take care of the zero case here in the case where the remote
        // has seen none of the operations of this client_id, and the zero value in the 
        // version vector corresponds to an operation. We may miss the 0th operation
        sv.set_min(local_client_id, remote_offset);
        // then get the updates with a happens after relationship to the modified state
        // vector
        let client_sync_step_2 = SyncMessage::SyncStep2(self.doc.transact().encode_diff_v1(&sv));
        client_sync_step_2
    }
    fn get_offset(&self) -> u32 {
        /*
        - this code for keeping the text area in sync with the yrs doc is kinda hacky
            - it seems like we treat the yrs doc as the source of truth for content 
                but the text area as the source of truth for the position of the cursor
            - we should either standardize the code for getting the cursor and the offset
                or we should make the yrs doc the source of truth fro the cursor position
            - also standardize the process of updating the text area and the cursor
         */
        let txn = self.doc.transact();
        // get the location of the cursor from the text area in terms of the YText
        let (row, col) = self.textarea.cursor();
        let text_repr = self.text.get_string(&txn);
        let lines: Vec<&str> = text_repr.lines().collect();
        let offset = self.cursor_to_offset(row, col, &lines);
        offset as u32
    }
    fn update_cursor(&mut self, offset: u32) {
        let txn = self.doc.transact();
        let text_repr = self.text.get_string(&txn);
        let lines: Vec<&str> = text_repr.lines().collect();
        let (row, col) = self.offset_to_cursor(offset.try_into().unwrap(), &lines);
        self.textarea.move_cursor(CursorMove::Jump(row.try_into().unwrap(), col.try_into().unwrap()));
    }
    fn process_update_inner(&mut self, update: Update) {
        let offset = self.get_offset();
        
        let mut txn = self.doc.transact_mut();
        let pos = self.text.sticky_index(
            &txn, offset.try_into().unwrap(), yrs::Assoc::Before
        ).unwrap();
        
        // make the update
        txn.apply_update(update).unwrap();
        // update the contents of the text area
        let status_message = self.text.get_string(&txn);
        self.textarea = TextArea::new(
            status_message.lines().map(String::from).collect(),
        );
        // change the cursors position such that it is still consistent after the update
        let offset = pos.get_offset(&txn).unwrap();
        drop(txn);
        self.update_cursor(offset.index);
    }
    fn process_remote_update(&mut self, update: Update) {
        self.process_update_inner(update);
    }
    // receive a message from the server with all the updates that the server has but the
    // client does not have 
    fn process_remote_sync_step_2(&mut self, update: Update) {
        self.process_update_inner(update);

    }
    fn get_status_message(&self) -> String {
        let txn = self.doc.transact();
        self.text.get_string(&txn)
    }

}


#[tokio::main]
async fn main() -> Result<(), Box<dyn std::error::Error>> {
    /*
    CHECKPOINT:
    - you were here
    - when creating a tui process, we are sending a client id to the server but the client id that we are using
      to create operations is different than that client id
    - we need to manually seed the yrs doc with the correct client id so that we can have alignment between the 
      client id used in the query parameter and the client id in the operation messages
    - factor out the config parsing code into its own struct and method
        - build
        - dump connection url
     */
    let args: Vec<String> = env::args().collect();
    let config = Config::build(&args).unwrap_or_else(|e| {
        println!("failed to parse string with {e}");
        process::exit(1);
    });

    // create a websocket connection with the update server
    let (ws_stream, _) = connect_async(config.dump_url()).await.expect(
        "failed to connect to websocket server"
    );
    let (mut write, mut read) = ws_stream.split();
    eprintln!("created websocket connection");

    let stdout = io::stdout();
    let mut stdout = stdout.lock();

    enable_raw_mode()?;
    crossterm::execute!(stdout, EnterAlternateScreen, EnableMouseCapture)?;
    let backend = CrosstermBackend::new(stdout);
    let mut term = Terminal::new(backend)?;

    let (mut editor_state, client_sync_step_1) = EditorState::new(Some(config.client_id.clone()));
    // create a crossterm event stream reader
    let mut term_reader = EventStream::new();

    write.send(Message::Binary(client_sync_step_1.encode_v1().into())).await?;

    loop {
        term.draw(|f| {
            let chunks = Layout::default()
                .direction(Direction::Vertical)
                .constraints([
                    Constraint::Ratio(1, 2),
                    Constraint::Ratio(1, 2),
                ])
                .split(f.area());

            f.render_widget(&editor_state.textarea, chunks[0]);

            let status = Paragraph::new(editor_state.get_status_message())
                .block(Block::default().borders(Borders::ALL).title("Status"));
            f.render_widget(status, chunks[1]);
        })?;

        tokio::select! {
            reader_result = term_reader.next() => {
                match reader_result {
                    Some(Ok(event)) => match event.into() {
                        Input { key: Key::Esc, .. } => break,
                        Input { key: Key::Enter, ..} => {
                            let update_message = editor_state.process_input("\n");
                            write.send(Message::Binary(update_message.encode_v1().into())).await?;
                        },
                        Input { key: Key::Char(c), .. } => {
                            let update_message = editor_state.process_input(&c.to_string());
                            write.send(Message::Binary(update_message.encode_v1().into())).await?;
                        },
                        Input { key: Key::Backspace, .. } => {
                            // implement backspace here lol
                        },
                        input => {
                            editor_state.textarea.input(input);
                        },
                    }
                    // TODO: handle these cases that take place when there
                    Some(Err(_)) => {},
                    None => {},
                }
            }
            // TODO: add a precondition guard that prevents us from reading from this 
            // stream once the websocket connection has closed
            ws_result = read.next() => match ws_result {
                // TODO: update this code to parse websocket sync message types
                Some(Ok(message)) => {
                    let data = message.into_data();
                    match SyncMessage::decode_v1(&data) {
                        Ok(SyncMessage::SyncStep1(state_vector)) => {
                            eprintln!("received a sync step one message with state vector: {:?}", state_vector);
                            let response = editor_state.process_remote_sync_step_1(state_vector);
                            eprintln!("produced response: {:?}", response);
                            write.send(Message::Binary(response.encode_v1().into())).await?;
                        },
                        Ok(SyncMessage::SyncStep2(encoded_update)) => {
                            let decoded_update = Update::decode_v1(&encoded_update)?;
                            eprintln!("received a sync step two message with decoded update: {:?}", decoded_update);
                            editor_state.process_remote_sync_step_2(decoded_update);
                        },
                        Ok(SyncMessage::Update(encoded_update)) => {
                            let decoded_update = Update::decode_v1(&encoded_update)?;
                            eprintln!("received an update message with decoded update: {:?}", decoded_update);
                            editor_state.process_remote_update(decoded_update);
                        },
                        Err(e) => {
                            eprintln!("failed to decode message with: {e}");
                        },
                    };
                },
                Some(Err(e)) => {
                    eprintln!("read from websocket with error: {e}");
                },
                None => { 
                    eprintln!("none value received from websocket");
                    return Ok(());
                },
            }
        }
    }

    disable_raw_mode()?;
    crossterm::execute!(
        term.backend_mut(),
        LeaveAlternateScreen,
        DisableMouseCapture
    )?;
    term.show_cursor()?;

    println!("Lines: {:?}", editor_state.textarea.lines());
    Ok(())
}