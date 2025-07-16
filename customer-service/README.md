# Customer Service

A microservice for managing customer subscriptions, billing, and brand preferences.

## Features

- Customer account management
- Subscription management with Stripe integration
- Payment method management
- Billing history and invoice generation
- Service area management
- **Customer brand preferences management**
- Webhook processing for Stripe events

## API Endpoints

### Authentication
All protected endpoints require a valid Bearer token in the Authorization header.

### Customer Management

#### Get Customer Information
```
GET /api/v3/customers/me
Authorization: Bearer <token>
```

#### Update Customer Information
```
PUT /api/v3/customers/me
Authorization: Bearer <token>
Content-Type: application/json

{
  "area_ids": [1, 2, 3]
}
```

#### Delete Customer Account
```
DELETE /api/v3/customers/me
Authorization: Bearer <token>
```

### Customer Brands Management

#### Get Customer Brands
```
GET /api/v3/customers/me/brands
Authorization: Bearer <token>
```

**Response:**
```json
{
  "customer_id": "customer_123",
  "brand_names": ["Nike", "Adidas", "Apple"]
}
```

#### Add Customer Brands
```
POST /api/v3/customers/me/brands
Authorization: Bearer <token>
Content-Type: application/json

{
  "brand_names": ["Nike", "Adidas"]
}
```

**Response:**
```json
{
  "message": "brands added successfully"
}
```

#### Update Customer Brands (Replace All)
```
PUT /api/v3/customers/me/brands
Authorization: Bearer <token>
Content-Type: application/json

{
  "brand_names": ["Nike", "Adidas", "Puma", "Apple"]
}
```

**Response:**
```json
{
  "message": "customer brands updated successfully"
}
```

#### Remove Customer Brands
```
DELETE /api/v3/customers/me/brands
Authorization: Bearer <token>
Content-Type: application/json

{
  "brand_names": ["Nike", "Adidas"]
}
```

**Response:**
```json
{
  "message": "brands removed successfully"
}
```
