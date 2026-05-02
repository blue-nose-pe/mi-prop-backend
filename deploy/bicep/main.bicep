// Mi Propósito 2.0 — infraestructura base en Azure.
// Aprovisiona: Azure SQL Server + 4 BDs (db_users, db_exams, db_keys, db_satisfaction)
//              + Azure Cache for Redis + Azure Key Vault + Azure Container Registry.
//
// Uso (PowerShell):
//   az login
//   az group create -n rg-miproposito -l eastus
//   az deployment group create -g rg-miproposito -f deploy/bicep/main.bicep \
//     -p sqlAdminLogin=miprop_admin sqlAdminPassword=<PWD-FUERTE>
//
// Después de aprovisionar, los secretos sensibles se leen del Key Vault
// montado en AKS via CSI driver. Los nombres de secret esperados están
// en cada Helm chart (values.yaml).

@description('Prefijo para todos los recursos. Debe ser único globalmente para SQL Server, ACR y Key Vault.')
@minLength(3)
@maxLength(15)
param namePrefix string = 'miproposito'

@description('Región Azure para todos los recursos.')
param location string = resourceGroup().location

@description('Login del administrador del Azure SQL Server.')
@minLength(4)
param sqlAdminLogin string

@description('Password del administrador del Azure SQL Server. Mínimo 12 caracteres.')
@secure()
@minLength(12)
param sqlAdminPassword string

@description('Tier de cada base de datos (Basic/Standard/Premium/GeneralPurpose).')
param sqlDbTier string = 'GeneralPurpose'

@description('SKU computacional. Para empezar: GP_S_Gen5_2 (serverless 2 vCore, auto-pause).')
param sqlDbSku string = 'GP_S_Gen5_2'

@description('SKU de Redis. C0 (250MB Basic) sirve para staging; C1+ para prod.')
@allowed(['Basic', 'Standard', 'Premium'])
param redisSkuName string = 'Standard'

@description('Capacidad Redis (C0=0, C1=1, C2=2 ...).')
param redisCapacity int = 1

@description('Object ID del principal/usuario admin que tendrá Key Vault Administrator.')
param keyVaultAdminObjectId string

// ----------------------------------------------------------------------
// Azure Container Registry (ACR) — para almacenar imágenes Docker.
// ----------------------------------------------------------------------
resource acr 'Microsoft.ContainerRegistry/registries@2023-11-01-preview' = {
  name: '${namePrefix}acr'
  location: location
  sku: {
    name: 'Standard'
  }
  properties: {
    adminUserEnabled: false
    publicNetworkAccess: 'Enabled'
  }
}

// ----------------------------------------------------------------------
// Azure SQL Server — host común para las 4 bases por servicio.
// ----------------------------------------------------------------------
resource sqlServer 'Microsoft.Sql/servers@2023-08-01-preview' = {
  name: '${namePrefix}-sql'
  location: location
  properties: {
    administratorLogin: sqlAdminLogin
    administratorLoginPassword: sqlAdminPassword
    version: '12.0'
    minimalTlsVersion: '1.2'
    publicNetworkAccess: 'Enabled'
  }
}

// Permite que servicios Azure (incluido AKS) accedan al SQL Server.
resource sqlFirewallAzure 'Microsoft.Sql/servers/firewallRules@2023-08-01-preview' = {
  parent: sqlServer
  name: 'AllowAllAzureServices'
  properties: {
    startIpAddress: '0.0.0.0'
    endIpAddress: '0.0.0.0'
  }
}

// ----------------------------------------------------------------------
// Bases de datos — una por microservicio.
// ----------------------------------------------------------------------
var databases = [
  'db_users'
  'db_exams'
  'db_keys'
  'db_satisfaction'
]

resource dbs 'Microsoft.Sql/servers/databases@2023-08-01-preview' = [for dbName in databases: {
  parent: sqlServer
  name: dbName
  location: location
  sku: {
    name: sqlDbSku
    tier: sqlDbTier
  }
  properties: {
    collation: 'SQL_Latin1_General_CP1_CI_AS'
    autoPauseDelay: 60          // serverless: pausa tras 60 min sin uso
    minCapacity: json('0.5')
    zoneRedundant: false
  }
}]

// ----------------------------------------------------------------------
// Azure Cache for Redis — usado por hubspot-service (cola BullMQ) y por
// gateway / analytics-service (rate-limit, dashboards cache).
// ----------------------------------------------------------------------
resource redis 'Microsoft.Cache/redis@2023-08-01' = {
  name: '${namePrefix}-redis'
  location: location
  properties: {
    sku: {
      name: redisSkuName
      family: redisSkuName == 'Premium' ? 'P' : 'C'
      capacity: redisCapacity
    }
    enableNonSslPort: false
    minimumTlsVersion: '1.2'
    publicNetworkAccess: 'Enabled'
  }
}

// ----------------------------------------------------------------------
// Azure Key Vault — guarda HUBSPOT_API_TOKEN, JWT secret, SQL passwords,
// Redis access keys. Los pods AKS lo leen vía Secrets Store CSI Driver.
// ----------------------------------------------------------------------
resource keyVault 'Microsoft.KeyVault/vaults@2023-07-01' = {
  name: '${namePrefix}-kv'
  location: location
  properties: {
    sku: {
      family: 'A'
      name: 'standard'
    }
    tenantId: subscription().tenantId
    enableRbacAuthorization: true
    enabledForDeployment: false
    enabledForTemplateDeployment: false
    enabledForDiskEncryption: false
    enableSoftDelete: true
    softDeleteRetentionInDays: 90
    publicNetworkAccess: 'Enabled'
  }
}

// Asigna 'Key Vault Administrator' al objectId provisto.
resource kvRoleAssignment 'Microsoft.Authorization/roleAssignments@2022-04-01' = {
  scope: keyVault
  name: guid(keyVault.id, keyVaultAdminObjectId, 'Key Vault Administrator')
  properties: {
    principalId: keyVaultAdminObjectId
    principalType: 'User'
    // Built-in role: Key Vault Administrator
    roleDefinitionId: subscriptionResourceId('Microsoft.Authorization/roleDefinitions', '00482a5a-887f-4fb3-b363-3b7fe8e74483')
  }
}

// ----------------------------------------------------------------------
// Outputs (para usar al generar los Helm values.yaml).
// ----------------------------------------------------------------------
output sqlServerFqdn string = sqlServer.properties.fullyQualifiedDomainName
output redisHost string = redis.properties.hostName
output redisSslPort int = redis.properties.sslPort
output keyVaultUri string = keyVault.properties.vaultUri
output acrLoginServer string = acr.properties.loginServer
output databaseNames array = databases
