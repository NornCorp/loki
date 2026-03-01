# Fake Data Types

Polymorph supports generating fake data using [gofakeit](https://github.com/brianvoe/gofakeit). Below are all available types you can use in resource field definitions.

## Basic Types

| Type | Example Output | Description |
|------|---------------|-------------|
| `uuid` | `550e8400-e29b-41d4-a716-446655440000` | UUID v4 |
| `bool` | `true` | Boolean value |
| `int` | `42` | Integer (supports min/max) |
| `decimal` | `123.45` | Decimal number (supports min/max) |
| `enum` | `"active"` | One of specified values |
| `ref` | `"uuid-reference"` | Reference to another resource's ID |
| `date` | `"2024-01-15"` | Date in YYYY-MM-DD format |
| `datetime` | `"2024-01-15T10:30:00Z"` | ISO 8601 datetime |

## Person

| Type | Example Output |
|------|---------------|
| `name` | `"John Doe"` |
| `firstname` | `"John"` |
| `lastname` | `"Doe"` |
| `gender` | `"male"` |
| `ssn` | `"123-45-6789"` |

## Address

| Type | Example Output |
|------|---------------|
| `address` | `"123 Main St"` |
| `street` | `"Main St"` |
| `city` | `"New York"` |
| `state` | `"California"` |
| `zip` | `"12345"` |
| `country` | `"United States"` |
| `latitude` | `40.7128` |
| `longitude` | `-74.0060` |

## Contact

| Type | Example Output |
|------|---------------|
| `email` | `"john@example.com"` |
| `phone` | `"5551234567"` |
| `phone_formatted` | `"(555) 123-4567"` |

## Company

| Type | Example Output |
|------|---------------|
| `company` | `"Acme Corp"` |
| `job_title` | `"Software Engineer"` |
| `job_descriptor` | `"Senior"` |
| `job_level` | `"Manager"` |

## Internet

| Type | Example Output |
|------|---------------|
| `url` | `"https://example.com"` |
| `domain` | `"example.com"` |
| `ipv4` | `"192.168.1.1"` |
| `ipv6` | `"2001:0db8:85a3::8a2e:0370:7334"` |
| `username` | `"johndoe123"` |
| `password` | `"aB3$xY9!pQ2@"` |
| `user_agent` | `"Mozilla/5.0..."` |
| `mac_address` | `"00:1B:44:11:3A:B7"` |

## Payment

| Type | Example Output |
|------|---------------|
| `credit_card` | `"4532123456789012"` |
| `credit_card_number` | `"4532123456789012"` |
| `credit_card_cvv` | `"123"` |
| `credit_card_exp` | `"01/25"` |
| `credit_card_type` | `"Visa"` |

## Words/Text

| Type | Example Output |
|------|---------------|
| `word` | `"example"` |
| `sentence` | `"The quick brown fox jumps."` |
| `paragraph` | `"Lorem ipsum dolor sit..."` |
| `question` | `"What is your name?"` |
| `quote` | `"To be or not to be"` |

## Product

| Type | Example Output |
|------|---------------|
| `product_name` | `"Wireless Mouse"` |
| `product_category` | `"Electronics"` |

## Color

| Type | Example Output |
|------|---------------|
| `color` | `"red"` |
| `hex_color` | `"#FF5733"` |
| `rgb_color` | `"rgb(255, 87, 51)"` |

## Miscellaneous

| Type | Example Output |
|------|---------------|
| `currency` | `"USD"` |
| `language` | `"English"` |
| `timezone` | `"America/New_York"` |
| `file_extension` | `"pdf"` |
| `mime_type` | `"application/pdf"` |

## Usage Example

```hcl
resource "user" {
  rows = 100

  field "id" {
    type = "uuid"
  }

  field "first_name" {
    type = "firstname"
  }

  field "last_name" {
    type = "lastname"
  }

  field "email" {
    type = "email"
  }

  field "phone" {
    type = "phone_formatted"
  }

  field "address" {
    type = "address"
  }

  field "city" {
    type = "city"
  }

  field "country" {
    type = "country"
  }

  field "job_title" {
    type = "job_title"
  }

  field "company" {
    type = "company"
  }

  field "bio" {
    type = "paragraph"
  }

  field "age" {
    type = "int"
    min  = 18
    max  = 65
  }

  field "account_status" {
    type   = "enum"
    values = ["active", "inactive", "suspended"]
  }
}
```
