use anyhow::Result;
use std::time::Duration;
use tonic::transport::{Channel, Endpoint, ClientTlsConfig};
use tonic::Request;

pub mod proto { tonic::include_proto!("stxn.io"); }
use proto::{request_registrator_service_client::RequestRegistratorServiceClient, PushRequestProto, UserEventProto, UserObjectiveProto, CallObjectProto};

#[tokio::main]
async fn main() -> Result<()> {
    // Hardcoded dev RR URL via nginx TLS proxy
    let rr_url = "https://stxn-cleanapp-dev.stxn.io:443";

    // Build HTTPS channel (tonic 0.13 negotiates TLS for https scheme)
    let mut endpoint = Endpoint::from_shared(rr_url.to_string())?
        .http2_keep_alive_interval(Duration::from_secs(30))
        .keep_alive_timeout(Duration::from_secs(10))
        .keep_alive_while_idle(true);
    let tls = ClientTlsConfig::new()
        .domain_name("stxn-cleanapp-dev.stxn.io".to_string())
        .with_enabled_roots();
    endpoint = endpoint.tls_config(tls)?;
    let channel: Channel = endpoint.connect().await?;

    let mut client = RequestRegistratorServiceClient::new(channel);

    // Mock event: zeroed app_id and intent, minimal objective
    let app_id = [0u8; 32];
    let intent_id = [1u8; 32];
    let chain_id: u64 = 21363;
    let user_objective = UserObjectiveProto {
        app_id: app_id.to_vec(),
        nonse: 1,
        chain_id,
        call_objects: vec![CallObjectProto {
            id: 0,
            chain_id,
            salt: vec![0; 32],
            amount: vec![0; 32],
            gas: vec![0; 32],
            address: vec![],
            skippable: true,
            verifiable: false,
            callvalue: vec![],
            returnvalue: vec![],
        }],
    };

    let event = UserEventProto {
        intent_id: intent_id.to_vec(),
        app_id: app_id.to_vec(),
        chain_id,
        block_number: 0,
        user_objective: Some(user_objective),
        additional_data: vec![],
    };

    println!("Connecting to {} and pushing mock event...", rr_url);
    let resp = client.push(Request::new(PushRequestProto { event: Some(event) })).await;
    match resp {
        Ok(ok) => {
            let r = ok.into_inner();
            println!("Push result: {:?}", r.result);
            println!("Sequence ID: {}", r.sequence_id);
        }
        Err(e) => {
            eprintln!("Push error: {:#}", e);
        }
    }

    Ok(())
}
