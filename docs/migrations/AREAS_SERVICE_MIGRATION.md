# Areas Service Migration

This document describes the migration of area-related endpoints from the main backend to a separate microservice.

## Overview

The following endpoints have been moved to a dedicated `areas-service`:

### Health Check
- `GET /health` - Health check endpoint

### API v3 Endpoints
- `POST /api/v3/create_or_update_area` - Create or update an area (🔒 **Bearer Token Required**)
- `GET /api/v3/get_areas` - Get areas with optional filtering
- `POST /api/v3/update_consent` - Update consent for an area
- `GET /api/v3/get_areas_count` - Get count of areas
- `DELETE /api/v3/delete_area` - Delete an area and all related data (🔒 **Bearer Token Required**)

## Authentication

The areas-service now includes authentication middleware for protected endpoints. The following endpoints require a valid Bearer token:

- `POST /api/v3/create_or_update_area`
- `DELETE /api/v3/delete_area`

### Configuration

Add the following environment variable to configure the auth-service URL:

```bash
export AUTH_SERVICE_URL=http://auth-service:8080
```

## Architecture Changes

### New Microservice Structure

```
```