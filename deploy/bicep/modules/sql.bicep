// Azure SQL Server con las 4 bases por microservicio + Private Endpoint
// en la subred `sql-pe-subnet`. NO acepta tráfico público.
//
// El AKS dialea por hostname privado; Private DNS Zone resuelve a IP
// privada de la subnet sin que la app tenga que conocer la IP.

@description('Prefix usado en todos los recursos.')
param namePrefix string

@description('Región Azure.')
param location string

@description('Subnet donde va el Private Endpoint del SQL.')
param sqlPeSubnetId string

@description('Resource ID de la VNet (para linkear la Private DNS Zone).')
param vnetId string

@description('SQL admin login.')
param sqlAdminLogin string

@description('SQL admin password.')
@secure()
param sqlAdminPassword string

@description('Tier de cada BD. Default GeneralPurpose.')
param sqlDbTier string = 'GeneralPurpose'

@description('SKU de cada BD. GP_S_Gen5_2 = serverless 2 vCore con auto-pause.')
param sqlDbSku string = 'GP_S_Gen5_2'

@description('Tags.')
param tags object = {}

// ----------------------------------------------------------------------
// SQL Server (sin acceso público — solo Private Endpoint)
// ----------------------------------------------------------------------
resource sqlServer 'Microsoft.Sql/servers@2024-05-01-preview' = {
  name: '${namePrefix}-sql'
  location: location
  tags: tags
  properties: {
    administratorLogin: sqlAdminLogin
    administratorLoginPassword: sqlAdminPassword
    version: '12.0'
    minimalTlsVersion: '1.2'
    publicNetworkAccess: 'Disabled' // ← solo via Private Endpoint
  }
}

// ----------------------------------------------------------------------
// 4 bases de datos, una por microservicio
// ----------------------------------------------------------------------
var dbNames = ['db_users', 'db_exams', 'db_keys', 'db_satisfaction']

resource dbs 'Microsoft.Sql/servers/databases@2024-05-01-preview' = [for dbName in dbNames: {
  parent: sqlServer
  name: dbName
  location: location
  tags: tags
  sku: {
    name: sqlDbSku
    tier: sqlDbTier
  }
  properties: {
    collation: 'SQL_Latin1_General_CP1_CI_AS'
    autoPauseDelay: 60      // serverless: pausa tras 60 min sin uso
    minCapacity: json('0.5')
    zoneRedundant: false
  }
}]

// ----------------------------------------------------------------------
// Private DNS Zone: privatelink.database.windows.net
// (estándar Microsoft para Azure SQL Private Endpoint)
// ----------------------------------------------------------------------
resource privateDnsZone 'Microsoft.Network/privateDnsZones@2024-06-01' = {
  name: 'privatelink.database.windows.net'
  location: 'global'
  tags: tags
}

resource dnsZoneVnetLink 'Microsoft.Network/privateDnsZones/virtualNetworkLinks@2024-06-01' = {
  parent: privateDnsZone
  name: 'vnet-link'
  location: 'global'
  properties: {
    registrationEnabled: false
    virtualNetwork: { id: vnetId }
  }
}

// ----------------------------------------------------------------------
// Private Endpoint en sql-pe-subnet
// ----------------------------------------------------------------------
resource privateEndpoint 'Microsoft.Network/privateEndpoints@2024-01-01' = {
  name: 'pe-${namePrefix}-sql'
  location: location
  tags: tags
  properties: {
    subnet: { id: sqlPeSubnetId }
    privateLinkServiceConnections: [
      {
        name: 'pe-sql-connection'
        properties: {
          privateLinkServiceId: sqlServer.id
          groupIds: ['sqlServer']
        }
      }
    ]
  }
}

// Vincular el Private Endpoint a la zona DNS para resolución automática.
resource peDnsGroup 'Microsoft.Network/privateEndpoints/privateDnsZoneGroups@2024-01-01' = {
  parent: privateEndpoint
  name: 'default'
  properties: {
    privateDnsZoneConfigs: [
      {
        name: 'sql-zone-config'
        properties: { privateDnsZoneId: privateDnsZone.id }
      }
    ]
  }
}

// ----------------------------------------------------------------------
// Outputs
// ----------------------------------------------------------------------
// El FQDN PRIVADO que las apps usan para conectarse desde el AKS.
output sqlServerFqdn string = '${sqlServer.name}.privatelink.database.windows.net'
// El FQDN público (no resoluble por dentro pero útil para info/scripts).
output sqlServerPublicFqdn string = sqlServer.properties.fullyQualifiedDomainName
output sqlServerName string = sqlServer.name
output databaseNames array = dbNames
