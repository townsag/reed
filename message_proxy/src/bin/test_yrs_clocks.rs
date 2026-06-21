use yrs::{self, ReadTxn, Text, Transact, Update, updates::{decoder::Decode, encoder::Encode}};
use anyhow::Result;

fn main() -> Result<()> {
    // create a yrs document
    let doc = yrs::Doc::new();
    let text = doc.get_or_insert_text("testing");
    // add an insertion on the text and log the insertions set
    {
        let mut txn = doc.transact_mut();
        text.insert(&mut txn, 0, "asdf");
        let update = txn.encode_update_v1();
        {
            let decoded_update = Update::decode_v1(&update)?;
            println!("insertions: {}", decoded_update.insertions(true));
            println!("deletions: {}", decoded_update.delete_set());
            println!("state vector lower: {:?}\n", decoded_update.state_vector_lower())
        }
    }
    // add a deletion on the text and log the deletion update on the insertion
    {
        let mut txn = doc.transact_mut();
        text.remove_range(&mut txn, 1, 2);
        let update = txn.encode_update_v1();
        {
            let decoded_update = Update::decode_v1(&update)?;
            println!("insertions: {}", decoded_update.insertions(true));
            println!("deletions: {}", decoded_update.delete_set());
            println!("state_vector_lower: {:?}\n", decoded_update.state_vector_lower());
        }
    }
    // add back in the deleted value
    {
        let mut txn  = doc.transact_mut();
        text.insert(&mut txn, 1, "s");
        let update = txn.encode_update_v1();
        {
            let decoded_update = Update::decode_v1(&update)?;
            println!("insertions: {}", decoded_update.insertions(true));
            println!("deletions: {}", decoded_update.delete_set());
            println!("state_vector_lower: {:?}\n", decoded_update.state_vector_lower());
        }
    }
    // delete the added back value
    {
        let mut txn  = doc.transact_mut();
        text.remove_range(&mut txn, 1, 1);
        let update = txn.encode_update_v1();
        {
            let decoded_update = Update::decode_v1(&update)?;
            println!("insertions: {}", decoded_update.insertions(true));
            println!("deletions: {}", decoded_update.delete_set());
            println!("state_vector_lower: {:?}\n", decoded_update.state_vector_lower());
        }
    }
    // delete the 0th value 
    {
        let mut txn  = doc.transact_mut();
        text.remove_range(&mut txn, 0, 1);
        let update = txn.encode_update_v1();
        {
            let decoded_update = Update::decode_v1(&update)?;
            println!("insertions: {}", decoded_update.insertions(true));
            println!("deletions: {}", decoded_update.delete_set());
            println!("state_vector_lower: {:?}\n", decoded_update.state_vector_lower());
        }
    }
    // create a local timestamp
    let local_sv = doc.transact().state_vector();
    println!("local sv: {:?}", local_sv);
    // create a second remote doc and sync the doc with this doc
    let remote_doc = yrs::Doc::new();
    let remote_text = remote_doc.get_or_insert_text("testing");
    // sync the local doc with the remote doc, inspect the update message to see how
    // delete sets are communicated
    let remote_timestamp = remote_doc.transact().state_vector();
    println!("remote sv: {:?}", remote_timestamp);
    let update = doc.transact().encode_diff_v1(&remote_timestamp);
    println!("update from remote doc: {:?}", Update::decode_v1(&update).unwrap());
    Ok(())
}
