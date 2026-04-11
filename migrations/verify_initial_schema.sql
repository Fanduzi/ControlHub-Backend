select table_name
from information_schema.tables
where table_schema = 'public'
  and table_name in (
    'users',
    'roles',
    'environments',
    'owners',
    'resources',
    'resource_relations',
    'resource_profiles_host',
    'resource_profiles_database_instance',
    'resource_profiles_database_cluster',
    'resource_profiles_service',
    'audit_events'
  )
order by table_name;
