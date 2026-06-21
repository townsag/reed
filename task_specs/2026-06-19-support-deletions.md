## Functional Descriptions:
- when an end user makes a deletion, that update should be:
    - durable
    - reflected locally in the tui
    - broadcast to all other connected clients and reflected locally for connected clients
    - sent to new clients on handshake

## Aside:
- deletions are both __associative__ and __commutative__ whereas insertions are strictly __commutative__
- both deletions and insertions are __idempotent__
- for this reason it makes sense to store deletions and insertions separately because order matters for insertions whereas order does not matter for deletions
    - deletions can be easily merged together at the database layer whereas insertions have to be merged at the application layer
- does it make sense to separate insertions from deletions at the messaging layer?
    - leaving the deletions in the update messages has the benefit of being able to tell when an update message that had both deletions and insertions was dropped at the message broker layer
- the desired behavior is to send the entire delete set upon handshake

## Multi Range size estimate
- the start and end of the range are stored in two int4s which each take up 4 bytes
- information describing if the range is inclusive or exclusive can be stored in 1 byte
- the multi range is probably of size 10(n) + k where n is the number of ranges and k is the overhead of the record metadata
    - the number of ranges is determined by how many unique locations of deletions there are in the document where insertions from a given client are deleted
- we store c multi-ranges where c is the number of clients on a document. This value is what is read by the server during handshake or lag-recovery
- the size of a deletion set is c(10(n) + k)
    - for 100 clients making deletions in 20 unique ranges and 8 bytes of overhead per record: 100(10*20 + 8) = 20,800 bytes or 20kb
    - this might be an issue, postgres has a fixed page size, any per client deletion set that does not fit in the fixed page size may have to be stored outside of the page, this causes and extra hop when reading that record 
        - I don't think this is too big of an issue, having 100 clients update a document is the extreme case
    - this might take about 750 microseconds (us) to read from disk or 50 us to send over local ethernet 1gb/s

## Technical Requirements:
- [ ] allow deletions in the tui
    - [ ] wire up deletions to the correct yrs operation then send the corresponding update messages over the websocket connection
- [ ] accept deletions in the server
    - [ ] any place that an update or client sync step two message can be received we should be able to accept messages that have no insertions in them but do have deletions
        - the previous behavior was to drop all messages that did not have any new insertions. The updated behavior should be to drop messages that have no new insertions and no deletions
        - [ ] client sync step two
        - [ ] update message
- [ ] make deletions durable
    - [ ] store the deletion set as an int8 multi range in postgres in a new deletions table
        - pk: (topic_id, client_id)
        - [ ] create a migration that adds the deletion table
        - [ ] add to the repository interface and implementation
            - [ ] read entire deletion set for a document
            - [ ] add deletion set to the document if the deletion set contains novel deletions, returning true if the deletion set contained novel deletions
            - [ ] decide if we want to use a write transaction to add update and if they should use the same sql statement or different sql statements
                - for data consistency, we probably want them to fail atomically, even if deletions are associative and idempotent
    - behavior
        - definition: client_delete_multirange @> new_delete_multirange
            - Does the client level deletion multirange contain the deletion set for this update message?
        - desired behavior
            - table
                | has update | has delete | new_offset >= prev_offset | client_delete_multirange @> new_delete_multirange | persist_update | persist_delete | broadcast message |
                | --- | --- | --- | --- | --- | --- | --- |
                | true ✅ | true ✅ | true ✅ | true ✅ | true ✅ | false ❌ | true ✅ |
                | true ✅ | true ✅ | true ✅ | false ❌ | true ✅ | true ✅ | true ✅ |
                | true ✅ | true ✅ | false ❌ | true ✅ | false ❌ | false ❌ | false ❌ |
                | true ✅ | true ✅ | false ❌ | false ❌ | false ❌ | true ✅ | true ✅ |
                | true ✅ | false ❌ | true ✅ | n/a | true ✅ | n/a | true ✅ |
                | true ✅ | false ❌ | false ❌ | n/a | false ❌ | n/a | false ❌ |
                | false ❌ | true ✅ | n/a | true ✅ | n/a | false ❌ | false ❌ |
                | false ❌ | true ✅ | n/a | false ❌ | n/a | true ✅ | true ✅ |
                | false ❌ | false ❌ | n/a | n/a | n/a | n/a | false ❌ |
            - persist_update = has_update && new_offset >= perv_offset
            - persist_delete = has_delete && !(client_delete_multirange @> new_delete_multirange)
            - broadcast messages = persist_update || persist_delete 
    - [ ] client sync step two messages
        - [ ] parse the deletion set out of the client sync step two message
            - [ ] client sync step two message should include only the deletion set for the client_id which is sending the message
        - [ ] parse the update set out of the client sync step two message
        - [ ] implement the behavior described in the table 
    - [ ] client update messages:
        - [ ] parse the deletion set out of the client update message
        - [ ] parse the update set out of the client update message
        - [ ] implement behavior described in the table
- [ ] broadcast to connected clients
    - [ ] messages that have either novel deletes or novel insertions should be broadcast to all connected clients
- [ ] sent to new clients on handshake
    - [ ] manually construct a server sync step two message that includes the entire delete set for that document

## Known limitations:
- if deletion only messages are dropped at the message bus level, we will have no way of knowing that they were dropped because deletion only messages do not have a contiguous sequence number from which we could detect holes
    - this would be rectified if we could detect that a message was dropped at the nats message bus level without having to use application layer sequence numbers 