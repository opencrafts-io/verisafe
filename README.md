# Verisafe
---

A very safe authentication system for the Academia platform,
made for people not pure profits

---

## The how and why for Verisafe
Verisafe is Academia's platform authentication system. It handles various
user and account operations to deliver high quality and secure authentication 
and authorization mechanisms to the platform.

Verisafe supports social oauth providers which include the following

1. Google
2. Spotify

Note that the above mentioned are fully implemented and work as expected

In the near future we are looking forward to integrate the following social providers

1. Apple
2. Microsoft


## Installation steps
Make sure you have go/golang installed (check with your os manual how to do so)

### Clone down the repo

```bash
git clone https://github.com/opencrafts-io/verisafe.git && cd verisafe
```

### Install the necessary dependencies
```bash
go mod tidy
```


### Run the application

Use the  `example.env` to create `.env` file setting up your client secrets
and client ids for the various oauth providers

```
cp example.env .env
```

The above command should setup a database for you compatible with the current version of verisafe as
at the moment you cloned


Then once you've configured the variables and migrations run the server using the following command

```
go run main.go
```

> Note that the above command by default will attempt to run migrations and launch the server.

To check whether everything went well you can try performing a simple get request to
```
http://127.0.0.1:8080/ping # Assuming you did not change the port or the address configuration
```

The above should reply with the following

```json
{
    "message": "he is risen"
}
```

Once this is done you can interact with the various endpoints inside the `docs/` directory


#### How to run tests
The application tries to follow test driven development (TDD) and to run the tests you can do so by

```bash
go test ./...
```


## Contributions
Contributions are welcome from anyone. Just make sure you adhere to our guidelines and remain respectful

