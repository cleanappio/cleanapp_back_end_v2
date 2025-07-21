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

### Customer Areas Management

#### Get Customer Areas
```
GET /api/v3/customers/me/areas
Authorization: Bearer <token>
```

**Response:**
```json
{
  "customer_id": "customer_123",
  "areas": [
    {
      "customer_id": "customer_123",
      "area_id": 1,
      "created_at": "2024-01-15T10:30:00Z"
    },
    {
      "customer_id": "customer_123",
      "area_id": 2,
      "created_at": "2024-01-16T14:20:00Z"
    }
  ],
  "count": 2
}
```

#### Add Customer Areas
```
POST /api/v3/customers/me/areas
Authorization: Bearer <token>
Content-Type: application/json

{
  "customer_id": "customer_123",
  "area_ids": [1, 2, 3]
}
```

**Response:**
```json
{
  "message": "areas added successfully"
}
```

#### Update Customer Areas (Replace All)
```
PUT /api/v3/customers/me/areas
Authorization: Bearer <token>
Content-Type: application/json

{
  "customer_id": "customer_123",
  "area_ids": [1, 2, 3, 4, 5]
}
```

**Response:**
```json
{
  "message": "areas updated successfully"
}
```

#### Delete Customer Areas
```
DELETE /api/v3/customers/me/areas
Authorization: Bearer <token>
Content-Type: application/json

{
  "customer_id": "customer_123",
  "area_ids": [1, 2]
}
```

**Response:**
```json
{
  "message": "areas deleted successfully"
}
```
