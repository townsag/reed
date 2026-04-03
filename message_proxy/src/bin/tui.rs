use crossterm::event::{DisableMouseCapture, EnableMouseCapture, EventStream};
use crossterm::terminal::{
    disable_raw_mode, enable_raw_mode, EnterAlternateScreen, LeaveAlternateScreen,
};
use ratatui::backend::CrosstermBackend;
use ratatui::layout::{Constraint, Direction, Layout};
use ratatui::widgets::{Block, Borders, Paragraph};
use ratatui::{Terminal};
use yrs::IndexedSequence;
use core::panic;
use std::io;
use std::env;
use tui_textarea::{Input, Key, TextArea, CursorMove};

use yrs::{
    Doc, GetString, Text, TextRef, Transact, Update,
    updates::decoder::Decode,
};
use tokio_tungstenite::{connect_async, tungstenite::protocol::Message};
use futures_util::{FutureExt, SinkExt, StreamExt};

struct EditorState<'a> {
    pub textarea: TextArea<'a>,
    doc: Doc,
    text: TextRef,
}

impl EditorState<'_> {
    fn new() -> Self {
        let doc = Doc::new();
        let text = doc.get_or_insert_text("text");
        let mut textarea = TextArea::default();
        textarea.set_block(
            Block::default()
                .borders(Borders::ALL)
                .title("Crossterm Minimal Example"),
        );
        return EditorState { textarea, doc, text }
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
            if line.len() + 1 + consumed_characters < offset {
                row += 1;
                consumed_characters += line.len() + 1;
            } else {
                col = offset - consumed_characters;
                break
            }
        }
        (row, col)
    }
    fn process_input(&mut self, input: &str) -> Vec<u8> {
        // txn holds an immutable borrow to self internally because we don't want to modify the
        // document other than with the txn
        
        // TODO: modify this code so that the mutable transaction is dropped before we try to 
        // replace the text area
        let mut txn = self.doc.transact_mut();
        // get the location of the cursor from the text area in terms of the YText
        let (row, col) = self.textarea.cursor();
        let text_repr = self.text.get_string(&txn);
        let lines: Vec<&str> = text_repr.lines().collect();
        let offset = self.cursor_to_offset(row, col, &lines);
        // apply the operation to the YText
        self.text.insert(&mut txn, offset.try_into().unwrap(), input);
        // update the text area lines to have the new content based on the YText representation
        let status_message = self.text.get_string(&txn);
        self.textarea = TextArea::new(
            status_message.lines().map(String::from).collect()
        );
        let updated_repr = self.text.get_string(&txn);
        let updated_lines: Vec<&str> = updated_repr.lines().collect();
        let (row, col) = self.offset_to_cursor(offset + 1, &updated_lines);
        self.textarea.move_cursor(CursorMove::Jump(row.try_into().unwrap(), col.try_into().unwrap()));
        
        txn.encode_update_v1()
    }
    fn process_remote_update(&mut self, update: Update) {
        let mut txn = self.doc.transact_mut();
        // record the cursors position before the update
        let (row, col) = self.textarea.cursor();
        let text_repr = self.text.get_string(&txn);
        let lines: Vec<&str> = text_repr.lines().collect();
        let offset = self.cursor_to_offset(row, col, &lines);

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
        let text_repr = self.text.get_string(&txn);
        let lines: Vec<&str> = text_repr.lines().collect();
        let (row, col) = self.offset_to_cursor(offset.index.try_into().unwrap(), &lines);
        self.textarea.move_cursor(CursorMove::Jump(row.try_into().unwrap(), col.try_into().unwrap()));

    }
    fn get_status_message(&self) -> String {
        let txn = self.doc.transact();
        self.text.get_string(&txn)
    }

}


#[tokio::main]
async fn main() -> io::Result<()> {
    let url = env::args().nth(1).unwrap_or_else(
        || panic!("this program requires at least one argument")
    );
    let stdout = io::stdout();
    let mut stdout = stdout.lock();

    enable_raw_mode()?;
    crossterm::execute!(stdout, EnterAlternateScreen, EnableMouseCapture)?;
    let backend = CrosstermBackend::new(stdout);
    let mut term = Terminal::new(backend)?;

    let mut  editor_state = EditorState::new();
    // create a websocket connection with the update server
    let (ws_stream, _) = connect_async(&url).await.expect(
        "failed to connect to websocket server"
    );
    let (mut write, mut read) = ws_stream.split();
    // create a crossterm event stream reader
    let mut term_reader = EventStream::new();

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
                            let update = editor_state.process_input("\n");
                            let _ = write.send(Message::Binary(update.into())).await;
                        },
                        Input { key: Key::Char(c), .. } => {
                            let update = editor_state.process_input(&c.to_string());
                            let _ = write.send(Message::Binary(update.into())).await;
                        }
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
                Some(Ok(message)) => {
                    let data = message.into_data();
                    if let Ok(update) = Update::decode_v1(&data) {
                        editor_state.process_remote_update(update);
                    }
                },
                Some(Err(e)) => {

                },
                None => {},
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