use crossterm::event::{DisableMouseCapture, EnableMouseCapture};
use crossterm::terminal::{
    disable_raw_mode, enable_raw_mode, EnterAlternateScreen, LeaveAlternateScreen,
};
use ratatui::backend::CrosstermBackend;
use ratatui::layout::{Constraint, Direction, Layout};
use ratatui::widgets::{Block, Borders, Paragraph};
use ratatui::{Terminal};
use std::io;
use tui_textarea::{Input, Key, TextArea, CursorMove};

use yrs::{Doc, GetString, Text, TextRef, Transact};

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
    fn process_input(&mut self, input: &str) {
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
    }
    fn get_status_message(&self) -> String {
        let txn = self.doc.transact();
        self.text.get_string(&txn)
    }

}


fn main() -> io::Result<()> {
    let stdout = io::stdout();
    let mut stdout = stdout.lock();

    enable_raw_mode()?;
    crossterm::execute!(stdout, EnterAlternateScreen, EnableMouseCapture)?;
    let backend = CrosstermBackend::new(stdout);
    let mut term = Terminal::new(backend)?;

    let mut  editor_state = EditorState::new();

    loop {
        term.draw(|f| {
            let chunks = Layout::default()
                .direction(Direction::Vertical)
                .constraints([
                    // Constraint::Min(1),      // textarea takes all available space
                    // Constraint::Length(3),   // status bar is fixed at 3 rows (border + 1 line)
                    Constraint::Ratio(1, 2),
                    Constraint::Ratio(1, 2),
                ])
                .split(f.area());

            f.render_widget(&editor_state.textarea, chunks[0]);

            let status = Paragraph::new(editor_state.get_status_message())
                .block(Block::default().borders(Borders::ALL).title("Status"));
            f.render_widget(status, chunks[1]);
        })?;
        match crossterm::event::read()?.into() {
            Input { key: Key::Esc, .. } => break,
            Input { key: Key::Enter, .. } => {
                editor_state.process_input("\n");
            },
            Input { key: Key::Char(c), .. } => {
                editor_state.process_input(&c.to_string());
            }
            input => {
                editor_state.textarea.input(input);
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