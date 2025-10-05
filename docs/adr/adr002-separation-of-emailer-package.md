---
id: adrs-adr002  
title: 'ADR002: Separation of emailer package'  
description: |  
 Refactor the monolithic emailer package into three independent packages: emailtemplater, emailprovider, and emailmanager to improve overall maintainability, testability, and extensibility of email functionality.  
---

## Decision

We will refactor the email system into three clean architecture packages:

### `emailtemplater` Package  
This package is responsible for email template generation and rendering  
- Generates HTML templates with variable substitution.  
- Configurable for different environments.  
- Zero dependencies on email sending logic.

### `emailprovider` Package  
This package is responsible for email sending abstraction  
- Defines a provider interface 
  - Keep support of SparkPost for initial implementation  
- Easy addition of new providers, i.e.

### `emailmanager` Package  
This package is responsible for email orchestration and business logic managment
- Combines the above packages and provides high-level APIs for sending emails.  
- Integrates with existing systems.

### Context

The original `emailer` package combined multiple functionalities, resulting in complexities with testing, limited extensibility for adding new providers, and tight coupling between template generation and sending logic.

### Architectural Boundaries

Here is the proposed boundaries

```
Application Layer (i.e, user, auth, accessmanager services, etc.)
             ↓
 emailmanager (Orchestration)
       ↙          ↘
emailtemplater   emailprovider
(Templates)      (Sending)
```

Each package will have its own models, errors, and configurations, ensuring testability and adherence to the single responsibility principle.

## Consequences

* Splitting `emailer` into three packages will improve testability and make it easier for others to shape for their unique use cases.
* Allow users to extend the base offering to better suit their requirements without having to alter core logic.
  * Easy addition of new providers (SendGrid, AWS SES, Mailgun, custom SMTP)
* Configuration will now need to be split across three packages instead of one, though it's worthwhile noting that each package's configuration is focused on its specific concerns.
* Users of GHAT(d) will need to understand the separation of concerns and know which package to use for different scenarios.
