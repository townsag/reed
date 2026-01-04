## Description:
- we need to be able to distinguish between different principal types at the token level so we can tell if a token corresponds to a guest before having to read from the database

## Technical Description V1 (deprecated)
- [ ] create two different token types
    - [ ] create a user token type
    - [ ] create a guest token type
    - [ ] modify the authentication middleware to be able to validate each token type 

## Technical Description V2:
- decided that having two different token claims structs was unnecessarily overcomplicated
- add a method to the custom claims struct that reads the type of the token (user or guest)
    - this is determined based on the presence of the userName field in the claims