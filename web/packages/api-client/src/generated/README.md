# blackstart API Client

Weather-driven outage risk and cascading failure analysis for electric power systems.

**Advisory only.** Nothing in this API commands anything. There is no control endpoint, no write path to any SCADA, EMS or ADMS, and no protocol client here can issue one — that is an architectural boundary rather than a phase-one limitation, and CI checks it on every commit. See docs/SAFETY.md.

Every number this API returns names the network it was computed from by content hash, the solver version that produced it, and the dataset version behind that. A figure you cannot trace to those three things is a bug.

Generated client for blackstart API unknown

## Installation

```bash
npm install @blackstart/api-client
# or
yarn add @blackstart/api-client
```

## Usage

```typescript
import { Client } from '@blackstart/api-client';

const client = new Client({
  baseURL: 'http://localhost:8080',
  auth: {
    bearerToken: 'your-token-here',
  },
});

// Use the client
// const result = await client.someMethod(...);
```

## Authentication

This API supports the following authentication methods:

### bearerAuth

- **Type**: http
- **Scheme**: bearer
- **Bearer Format**: JWT
- **Usage**: Pass the token in the `Authorization` header as `Bearer <token>`

### apiKey

- **Type**: apiKey
- **Location**: header
- **Header Name**: apiKey


## API Overview

- **Total Endpoints**: 48
- **REST Endpoints**: 48

### Servers

- **http://localhost:8080**: Development server
- **http://localhost:8080**: This instance

## License

[Apache-2.0](https://www.apache.org/licenses/LICENSE-2.0.html)

