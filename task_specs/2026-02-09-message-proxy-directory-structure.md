## Description:
- refactor the message proxy service to be more:
    - testable
    - soc
- These are good examples of well structured axum projects:
    - https://github.com/tokio-rs/axum/tree/main/examples


## Notes:
- not declaring this module as public means that it can be viewed by other modules in this crate but other crates cannot view this module
- making the function inside the module public means that it can be used outside the scope of that module
- modules are a lot like files in a filesystem