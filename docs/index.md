# Introduction to Verisafe

Verisafe pronounced as 'Very Safe' is an Authentication and Authorization service for the Academia platform.

Its primary functions are as follows.

1. Interfaces between other Oauth providers such as Google and Apple
2. Manages user accounts and user profiles on the Academia platform
3. Manages user roles and permissions on the platform
4. Provides user information to other services on the Academia platform.
5. Providence of supported institutions on the platform

## The need for Verisafe

The Academia platform is huge with multiple services requiring information about core critical components such as 

- User information
- User roles and permissions
- A way to authenticate a user against their auth provider such as Google
- A centralized place for profile and user management
- A way to authorize service to service communication.

If every service was to handle these mentioned issues, the code bases would grow, chances of errors would also grow 
exponentially with also the risk of services having stale or incorrect information leading to either a buggy or 
erroneous processing of data leading to incorrect results or information on the platform

To counter this the Verisafe service was created and delegated with the role of being the only source of truth for 
the above mentioned roles.

> At any given point, Verisafe's information is final regarding the roles it plays and is not to be debated.


## Environments

Verisafe has three primary roles of operation

 - Development mode - Run locally on the developer's machine
 - Staging mode - Deployed upon a push to the remote repository triggered by CI/CD pipeline
 - Production mode - The client facing version of the project thats  merged into the main branch of the project

 > More about the environments will be discussed later

## Documentation

New to the codebase? Start with **[How auth works](HOW_AUTH_WORKS.md)** — it explains logging in,
tokens, permissions, and third-party access end to end, in plain language.

Reference material, once you know your way around:

| Document | Covers |
|---|---|
| [HOW_AUTH_WORKS.md](HOW_AUTH_WORKS.md) | Guided walkthrough of the whole auth system |
| [AUTHENTICATION.md](AUTHENTICATION.md) | Login endpoints and the JWT/refresh-token lifecycle |
| [OAUTH_SCOPES.md](OAUTH_SCOPES.md) | Third-party grants, the token broker, incremental scopes |
| [RBAC.md](RBAC.md) | Roles, permissions, and how they are assigned |
| [SERVICE_TOKENS.md](SERVICE_TOKENS.md) | Service-to-service API keys |
| [BOT_ACCOUNT_CREATION.md](BOT_ACCOUNT_CREATION.md) | Creating bot accounts |
| [RABBITMQ_INTEGRATION.md](RABBITMQ_INTEGRATION.md) | Event publishing and consumption |
| [setup.md](setup.md) | Getting the service running locally |
| [adrs/](adrs/) | Why things are the way they are |




