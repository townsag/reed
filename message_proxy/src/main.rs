use tokio;
use message_proxy::run;

#[tokio::main]
async fn main() {
   run().await 
}