service "postgres" "db" {
  listen = "0.0.0.0:5432"

  auth {
    users    = { "app" = "secret" }
    database = "myapp"
  }

  table "user" {
    rows = 100
    column "id"    { type = "uuid" }
    column "name"  { type = "name" }
    column "email" { type = "email" }
  }

  table "order" {
    rows = 50
    column "id" { type = "uuid" }

    column "user_id" { type = "uuid" }

    column "total" {
      type = "decimal"
      min  = 10
      max  = 500
    }

    column "status" {
      type   = "enum"
      values = ["pending", "shipped", "delivered"]
    }

    column "created_at" { type = "datetime" }
  }

  # Custom query override
  # ${1} captures the column list (*), ${2} captures the status value
  query "select * from users where status = *" {
    from_table = "user"
    where      = "status = ${2}"
  }
}
