mock_provider "pmon" {}

override_data {
  target = data.pmon_datasources.all
  values = {
    datasources = [
      { name = "private-ro", tags = ["service:private"], engine_version = null },
      { name = "shared-ro", tags = ["service:shared"], engine_version = "8.0" },
    ]
  }
}

override_data {
  target = data.pmon_groups.all
  values = {
    groups = [
      { name = "reviewers", source = "OIDC", role_names = ["datasource:private-ro:reviewer"] },
      { name = "developers", source = "OIDC", role_names = [] },
    ]
  }
}

run "inventory_entries_keep_their_own_names_tags_and_roles" {
  command = plan

  assert {
    condition = (
      length(data.pmon_datasources.all.datasources) == 2
      && data.pmon_datasources.all.datasources[0].name == "private-ro"
      && data.pmon_datasources.all.datasources[0].tags == tolist(["service:private"])
      && data.pmon_datasources.all.datasources[0].engine_version == null
      && data.pmon_datasources.all.datasources[1].name == "shared-ro"
      && data.pmon_datasources.all.datasources[1].tags == tolist(["service:shared"])
      && data.pmon_datasources.all.datasources[1].engine_version == "8.0"
    )
    error_message = "Datasource overrides must preserve distinct entries and nullable metadata."
  }

  assert {
    condition = (
      length(data.pmon_groups.all.groups) == 2
      && data.pmon_groups.all.groups[0].name == "reviewers"
      && data.pmon_groups.all.groups[0].role_names == tolist(["datasource:private-ro:reviewer"])
      && data.pmon_groups.all.groups[1].name == "developers"
      && length(data.pmon_groups.all.groups[1].role_names) == 0
    )
    error_message = "Group overrides must preserve distinct entries and empty role lists."
  }
}
