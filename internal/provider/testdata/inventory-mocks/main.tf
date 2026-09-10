terraform {
  required_providers {
    pmon = {
      source = "ridi-oss/pmon"
    }
  }
}

data "pmon_datasources" "all" {}

data "pmon_groups" "all" {}
