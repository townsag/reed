## Functional Requirements:
- many clients should be able to edit the same document simultaneously, even if there are more clients than can connect to one instance of the message proxy service

## Technical Requirements:
- [x] add nats core to the message proxy subsystem docker compose file
- [x] create a nats core client on the message proxy service instance
- [ ] send updates to nats core:
    - [x] update the BrokerMessage struct and routable trait to differentiate between the topic_id and the client_id
        - this is so we can tell which subject to publish a messages on when it gets to the nats client 
    - [x] add the nats async client to the broker struct so that it may be added to the wrapped senders
        - [x] update the broker builder to take the nats client
    - [x] add the nats client to the wrapped sender
        - use the Futures crate now_or_never() function to only write to the nats client if the nats client is ready to accept the write without blocking
        - [x] record that the message was dropped when writing to the nats client and why it was dropped
    - [ ] record metrics
        - [ ] when do we drop messages that are sent to the nats client mpsc channel
            - [ ] why they are dropped
        - [ ] what is the average length of the nats client mpsc channel
            - this is not available information
        - [ ] todo
- [ ] receive updates from nats core:
    - [x] update the broker to have a second sender type that we use to receive messages from nats core
        - we keep track of the count of receivers when deciding when to remove the broadcast Sender from the hashmap of topic_ids and broadcast senders
        - for this reason we do not have to worry about extra senders increasing the sender count
        - ultimately decided to use regular broadcast senders, this reuses the existing code we have for sending and receiving messages
    - [x] update the broker to have ownership over the nats client value
    - [x] update the broker to create nats core subscribers
        - [ ] when a new broadcast channel is created:
            - a new nats core subscriber should be created for that broadcast channels subject
            - an async task should be created that reads from the subscriber and writes to the broadcast channel sender
                - this allows receivers on that broadcast channel to get messages on that subject from other instances of the message proxy service
            - the spawned async task should have ownership of the nats core subscriber
            - the topics map should hold the spawn handle of this async task as well as the broadcast sender
        - [x] when the last wrapped receiver is dropped and we need to clean up the resources for that topic:
            - use the join handle for that topics async task to stop the async task
            - it is not necessary to flush the remaining messages in the broadcast channel, etc. because we are cleaning up the last receiver. Any sent messages would have nobody to read them
        - [ ] drop messages that originated from this instance?
    - [ ] record metrics:
        - [ ] when do we fail to deserialize messages that are read from the nats subscriber
        - [ ] how many messages are we receiving from the nats subscriber per minute, per instance

## Things to test:
- does the subscriber actually get dropped when the last task for that topic is dropped?