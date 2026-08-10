# The shipped production presets read every column unless it is tagged pii, so an unclassified
# production datasource is read in full cleartext. These tags are what make that preset bite.
resource "pmon_column_classification" "users" {
  datasource = "example-prod-rw"

  columns = [
    {
      schema       = "example"
      table        = "users"
      column       = "email"
      tags         = ["pii"]
      mask_fn_name = pmon_mask_fn.email.name
    },
    {
      schema = "example"
      table  = "users"
      column = "phone"
      tags   = ["pii"]
    },
  ]
}
