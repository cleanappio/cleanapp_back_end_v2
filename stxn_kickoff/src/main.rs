use std::sync::Arc;

use clap::{arg, Parser};
use ethers::{
    abi::AbiEncode,
    middleware::MiddlewareBuilder,
    prelude::abigen,
    providers::{Provider, Ws},
    signers::{LocalWallet, Signer},
    types::{Address, Bytes},
};
use fatal::fatal;
use keccak_hash::keccak;

abigen!(
    KITNDisbursementScheduler,
    "./abi/KITNDisburmentScheduler.sol/KITNDisburmentScheduler.json";

    LaminatedProxy,
    "./abi/LaminatedProxy.sol/LaminatedProxy.json";

    Laminator,
    "./abi/Laminator.sol/Laminator.json";
);

#[derive(Parser, Debug)]
pub struct Args {
    #[arg(long)]
    pub chain_id: u64,

    #[arg(long)]
    pub ws_chain_url: String,

    #[arg(long)]
    pub laminator_address: Address,

    #[arg(long)]
    pub call_breaker_address: Address,

    #[arg(long)]
    pub kitn_disbursement_scheduler_address: Address,

    #[arg(long)]
    pub cleanapp_wallet_private_key: LocalWallet,
}

#[tokio::main]
async fn main() {
    let args = Args::parse();
    let cleanapp_wallet = args
        .cleanapp_wallet_private_key
        .with_chain_id(args.chain_id);

    println!(
        "Connecting to the chain with URL {} ...",
        args.ws_chain_url.as_str()
    );
    let cleanapp_provider = Provider::<Ws>::connect(args.ws_chain_url.as_str()).await;
    if cleanapp_provider.is_err() {
        fatal!(
            "Failed connection to the chain: {}",
            cleanapp_provider.err().unwrap()
        );
    }
    println!("Connected successfully!");

    let cleanapp_wallet_address = cleanapp_wallet.address();
    let cleanapp_provider = Arc::new(cleanapp_provider.ok().unwrap().with_signer(cleanapp_wallet));

    let laminator_contract = Laminator::new(args.laminator_address, cleanapp_provider.clone());
    let laminated_proxy_address = laminator_contract
        .compute_proxy_address(cleanapp_wallet_address)
        .await;
    if let Err(err) = laminated_proxy_address {
        fatal!("Cannot get laminated proxy address: {}", err);
    }
    let laminated_proxy_address = laminated_proxy_address.unwrap();
    println!(
        "Use laminated proxy at the address {}",
        laminated_proxy_address
    );

    let should_continue_object = CallObject {
        amount: 0.into(),
        addr: args.kitn_disbursement_scheduler_address,
        gas: 10000000.into(),
        callvalue: KITNDisbursementSchedulerCalls::ShouldContinue(ShouldContinueCall)
            .encode()
            .into(),
    };
    let call_objects = vec![
        CallObject {
            amount: 0.into(),
            addr: args.kitn_disbursement_scheduler_address,
            gas: 10000000.into(),
            callvalue: KITNDisbursementSchedulerCalls::DisburseKITNs(DisburseKITNsCall)
                .encode()
                .into(),
        },
        CallObject {
            amount: 0.into(),
            addr: laminated_proxy_address,
            gas: 10000000.into(),
            callvalue: LaminatedProxyCalls::CopyCurrentJob(CopyCurrentJobCall {
                delay: 0.into(),
                should_copy: should_continue_object.encode().into(),
            })
            .encode()
            .into(),
        },
        CallObject {
            amount: 33.into(),
            addr: args.call_breaker_address,
            gas: 10000000.into(),
            callvalue: Bytes::new(),
        },
    ];

    let solver_data = vec![SolverData {
        name: "CRON".to_string(),
        datatype: 2,
        value: "0 */5 * * * *".to_string(),
    }];

    match laminator_contract.push_to_proxy(
        call_objects.encode().into(),
        1,
        *keccak("CLEANAPP.SCHEDULER".encode()).as_fixed_bytes(),
        solver_data,
    ).send().await {
        Ok(pending) => {
            println!("Laminator.pushToProxy sent to execution, txhash: {}", pending.tx_hash());
            match pending.await {
                Ok(receipt) => {
                    if let Some(receipt) = receipt {
                        match receipt.status {
                            Some(status) => {
                                println!("Transaction status: {}", status);
                            }
                            None => {
                                println!("No transaction status");
                            }
                        }
                    }
                }
                Err(err) => {
                    println!("Error executing Laminator.pushToProxy: {}", err);
                }
            }
        }
        Err(err) => {
            println!("Error sending Laminator.pushToProxy: {}", err);
        }
    }
}
