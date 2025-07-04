# Setup for the CleanApp developer

## Configuring SSH access to VMs

This configuration is to be done once.

1.  Generate an SSH keys pair
    ```
    ssh-keygen -t rsa -f .ssh/<you>-cleanapp-io -C <you>
    ```

1.  Upload a public key to Google cloud
    ```
    gcloud compute os-login ssh-keys add --key-file=.ssh/<you>-cleanapp-io.pub
    ```

1.  Set up the dev machine
    *   Login to the dev VM with SSH
        ```
        ssh -i ~/.ssh/<you>-cleanapp-io <you>_cleanapp_io@34.132.121.53
        ```
    *   Grant ssh permission to the deployer account
        ```
        sudo nano /home/deployer/.ssh/authorized_keys
        ```
        Add the previously generated public key to the end of the file.
    *   Login as deployer to check that it works
        ```
        ssh -i ~/.ssh/<you>-cleanapp-io deployer@34.132.121.53
        ```
1.  Set up prod machine
    *   Same as dev machine, just use the IP address 34.122.15.16

## Deploying the CleanApp component

Here is a deployment documentation. Using the frontend deployment as an example.

1.  Clone the frontend repository.
    ```
    git clone https://github.com/cleanappio/cleanapp-frontend.git
    ```

2.  Make your changes.

### Dev deployment

Run the build & deploy script.
```
./build_image.sh -e dev --ssh-keyfile ~/.ssh/<you>-cleanapp-io
```

### Production deployment

Run the build & deploy script.
```
./build_image.sh -e prod --ssh-keyfile ~/.ssh/<you>-cleanapp-io
```

## Setting up a new CleanApp VM

1.  Enable OS login on VMs

1.  Configure the deployer user
    1.  Login to the VM as yourself, either via Cloud SSH or using its external IP address with your key.
        ```
        ssh -i .ssh/<you>-cleanapp-io <you>_cleanapp_io@<VM IP address>
        ```

    1.  Create the deployer user.
        ```
        sudo adduser deployer
        ```
        The command `adduser` will ask for creating a password. You can create any of them, it won't be used.

    1.  Configure passwordless sudo for deployer.
        *   Run the `sudo visudo`
        *   Add the following line after the `%sudo   ALL=(ALL:ALL) ALL`
            ```
            deployer ALL=(ALL) NOPASSWD:ALL
            ```
        *   Save changes

    1.  Enable users for logging in as deployer.
        *   Add public SSH keys of all users you want to grant permission to into the file `/home/deployer/.ssh/authorized_keys`
        *   Set proper permissions
            ```
            sudo chown -R deployer:deployer /home/deployer/.ssh
            sudo chmod 700 /home/deployer/.ssh
            sudo chmod 600 /home/deployer/.ssh/authorized_keys
            ```

    1.  Login to the VM as deployer
        ```
        ssh -i .ssh/<you>-stxn-cloud deployer@<VM IP address>
        ```

    1.  Configure the deployer for docker communications
        ```
        gcloud auth configure-docker us-central1-docker.pkg.dev
        ``` 
        That will create a configuration file .docker/config.json.

    1.  Configure the service account
        ```
        deployer@vampfun-dev:~$ gcloud config set account cleanapp@cleanup-mysql-v2.iam.gserviceaccount.com
        ```

    1. Activate the service account
        *   Generate a new keypair for the account
        *   Copy the keypair file to the VM
        *   Activate the account
            ```
            gcloud auth activate-service-account cleanapp@cleanup-mysql-v2.iam.gserviceaccount.com --key-file=<keypair file>
            ```
        *   Delete the keypair file after activation
