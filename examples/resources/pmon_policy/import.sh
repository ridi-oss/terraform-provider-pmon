# Policies are name-keyed. Shipped system: policies cannot be imported: they are immutable,
# so read them with the pmon_policy data source instead.
terraform import pmon_policy.service_fence 'service:fence'
