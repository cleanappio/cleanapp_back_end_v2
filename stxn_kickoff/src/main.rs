use std::{str::FromStr, sync::Arc};

use clap::{arg, Parser};
use ethers::{
    abi::AbiEncode,
    middleware::MiddlewareBuilder,
    prelude::abigen,
    providers::{Provider, Ws},
    signers::{LocalWallet, Signer},
    types::{Address, Bytes}, utils::parse_units,
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

    #[arg(long)]
    pub cron_schedule: String,
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

    let call_breaker_amount_wei = parse_units("0.01", "ether").ok().unwrap();

    // let should_continue_object = CallObject {
    //     amount: 0.into(),
    //     addr: args.kitn_disbursement_scheduler_address,
    //     gas: 10000000.into(),
    //     callvalue: KITNDisbursementSchedulerCalls::ShouldContinue(ShouldContinueCall)
    //         .encode()
    //         .into(),
    // };

    let should_continue_hardcoded = Bytes::from_str("0x0000000000000000000000000000000000000000000000000000000000000020000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000009896800000000000000000000000007e485fd55cedb1c303b2f91dfe7695e72a53739900000000000000000000000000000000000000000000000000000000000000800000000000000000000000000000000000000000000000000000000000000004aeec050100000000000000000000000000000000000000000000000000000000").unwrap();

    let call_objects: Bytes = vec![
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
                should_copy: should_continue_hardcoded,
            })
            .encode()
            .into(),
        },
        CallObject {
            amount: call_breaker_amount_wei.into(),
            addr: args.call_breaker_address,
            gas: 10000000.into(),
            callvalue: Bytes::new(),
        },
    ].encode().into();

    let solver_data = vec![SolverData {
        name: "CRON".to_string(),
        datatype: 2,
        value: args.cron_schedule,
    }];

    match laminator_contract.push_to_proxy(
        call_objects,
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
