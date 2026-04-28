## Bug Description:
- The order in which the websocket server writer task receives the client sync step 1 message and server sync step message message should not matter
- preceding this PR, the writer task stops listening on the sync channel for reader events after the writer task receives the client sync step one message, meaning that it could potentially miss the server sync step one message
- the ways that this can be fixed and the tradeoffs involved are documented in this block comment:


/*
Originally this code was part of the reader to preserve separation of concerns. The read task is 
concerned with which operations the server has received from the client. The writer is concerned with 
which operations the client has received from the server. This code was moved from the reader to the
writer to simplify the implementation of the writer. If this message is created in the writer. It 
does not need to be communicated between the reader and the writer. The writer implementation does
not need to worry about listening for Server Sync Step 1 reader events when it is in the hot path.
This eliminates the possibility of a race condition between the writer and the reader in which the
writer stops listening for reader events (because it is in the hot path) before the reader has 
the time to transmit the server sync step 1 message
*/
/*
In the future if we want to reinstate separation of concerns:
- the server sync step one message should be returned by the read state machine constructor
- Create the reader and the writer state machines in the handle_socket function.
- pass the server sync step one message into the writer task along with the writer state machine
*/
/*
Tradeoff:
- the writer task has to wait to construct and send a server sync step one message before it can 
    receive an process a client sync step one message. This means that potentially long database 
    read times could stall the writer handshake process even though reading the last received message
    for this client_id is independent from the writer sync process.
*/