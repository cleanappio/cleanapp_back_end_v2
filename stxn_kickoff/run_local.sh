chain_id=21363
ws_chain_url=wss://service.lestnet.org:8888/
laminator_address=0x36aB7A6ad656BC19Da2D5Af5b46f3cf3fc47274D
call_breaker_address=0x23912387357621473Ff6514a2DC20Df14cd72E7f
kitn_disbursement_scheduler_address=0x7E485Fd55CEdb1C303b2f91DFE7695e72A537399
cleanapp_wallet_private_key=$(gcloud secrets versions access 1 --secret="KITN_PRIVATE_KEY_PROD")

cargo run \
  -- \
  --chain-id=${chain_id} \
  --ws-chain-url=${ws_chain_url} \
  --laminator-address=${laminator_address} \
  --call-breaker-address=${call_breaker_address} \
  --kitn-disbursement-scheduler-address=${kitn_disbursement_scheduler_address} \
  --cleanapp-wallet-private-key=${cleanapp_wallet_private_key} \

test -d target && rm -rf target
