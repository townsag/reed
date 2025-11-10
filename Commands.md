## Run the docker compose file
```bash
# if you have just checked out the project, run this line
chmod +x ./init_scripts/*.sh
# next, make sure your .env file is present and matches .env.example
docker compose build
docker compose up
```