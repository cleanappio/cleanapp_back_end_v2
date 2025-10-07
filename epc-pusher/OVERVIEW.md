
# How it works

You want an address for each Ethereum Place Code (EPC), potentially with some smart contract functions down the line, and you dont want to administrate a private key for each EPC. This presents a problem, since the address of EPC contract creation depends either on a nonce of the creating address (the order of which will be unknown, making it impossible to predict the address) or the hash of the contract code (which you want to be able to upgrade, again making it impossible to predict); so, there is a method called "CREATE3" which uses some trickery to get a predictable contract address using only a pre-determined key, which you can then mint if/whenever you need to.

The process uses an associated smart contract factory ([link](https://github.com/ssadler/epc-contracts)) which can be upgraded to mint the EPC contracts at such a time as we know what they will be, but in the meantime, it is able to return an EPC address given a key. The EPC key is stored in the epc_contracts table. It is prefixed with a campaign prefix so you can represent different sources.

So, of the things remaining to do are to deploy the contracts to get the minting contract address, put that into an environment variable for the backend process, load the contract into a client side EVM interpreter so the contract can be called in-process to get the EPC address for a given key.

I'll get around to all this pretty soon anyway
Oh, the EPC key is different depending on what it represents, but for the purposes of cleanapp reports, it'll be something like: cleanapp/$BRAND_NAME. The process that checks for EPC messages to send out has it's own "campaign" in the epc_campaigns table. If we were to for example do a batch process to create EPCs for Google-Plus shortcodes, we might create an EPC with the key: gplus/$SHORTCODE, and that would be a different campaign.
Each campaign has a slug which is an internal ID, distinct from the campaign key prefix, so you can have the same key prefix for different campaigns. The campaigns allow you to track batches of messages that you have sent out and why you sent them, which template you used etc.
