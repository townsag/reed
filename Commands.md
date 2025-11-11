## Run the docker compose file
```bash
# if you have just checked out the project, run this line
chmod +x ./init_scripts/*.sh
# next, make sure your .env file is present and matches .env.example
docker compose build
docker compose up
```

## To view the traces in your browser
```bash
cd .
docker compose up
# in a second terminal window
cd user_service
go run call_user_service.go
# view the traces at localhost:3000 
```