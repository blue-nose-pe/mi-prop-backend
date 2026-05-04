// =====================================================================
// Mi Propósito 2.0 — INFRAESTRUCTURA AZURE COMPLETA
// =====================================================================
// Orquesta todos los módulos para reproducir la arquitectura del
// diagrama del cliente:
//
//   ┌────────────┐    ┌────────────────────────────────────────┐
//   │ WAF subnet │───▶│ vnet-ucsp-aks                          │
//   │ (App Gw)   │    │  ├── lb-subnet      (Load Balancers)   │
//   │ [opcional] │    │  ├── aks-subnet     (AKS pods)         │
//   └────────────┘    │  └── sql-pe-subnet  (SQL endpoint)     │
//                     └────────────────────────────────────────┘
//   Recursos auxiliares: Key Vault · Storage · ACR · Redis · Azure DevOps
//
// Uso:
//   az deployment group create -g rg-miproposito \
//     -f deploy/bicep/main.bicep \
//     -p sqlAdminLogin=miprop_admin \
//     -p sqlAdminPassword=<pwd> \
//     -p adminObjectId=<az ad signed-in-user show --query id -o tsv>

// ----- Parámetros principales -----
@description('Prefix global. Debe ser único en Azure (lo usa ACR + KV).')
@minLength(3)
@maxLength(15)
param namePrefix string = 'miproposito'

@description('Región Azure.')
param location string = resourceGroup().location

@description('SQL admin login.')
@minLength(4)
param sqlAdminLogin string

@description('SQL admin password — mínimo 12 chars con may/min/digit/símbolo.')
@secure()
@minLength(12)
param sqlAdminPassword string

@description('Object ID del usuario que despliega (recibe Key Vault Administrator).')
param adminObjectId string

@description('Activar Application Gateway + WAF v2. Por default OFF — costoso (~$200-300/mes). El cliente lo activa cuando esté listo a pagarlo.')
param enableAppGateway bool = false

@description('Aprovisionar Azure SQL Server gestionado (con Private Endpoint). Default true. Para staging en suscripciones gratuitas que bloquean SQL, pasar false → se usa SQL Server in-cluster (StatefulSet en AKS).')
param enableAzureSql bool = true

@description('Aprovisionar Azure Cache for Redis gestionado. Default true. Para staging, pasar false → se usa Redis in-cluster (Pod en AKS).')
param enableAzureRedis bool = true

@description('SKU de la VM de los nodos AKS. Default Standard_D2s_v3 (prod, 8 GB RAM). Para staging usar Standard_B2s (4 GB).')
param aksNodeVmSize string = 'Standard_D2s_v3'

@description('Cantidad de nodos AKS. Default 3 (HA). Para staging/demo 2 alcanza.')
@minValue(1)
@maxValue(10)
param aksNodeCount int = 3

@description('SKU de Redis. Default Standard (SLA 99.9%). Para staging Basic ahorra ~$85/mes pero no tiene SLA.')
@allowed(['Basic', 'Standard', 'Premium'])
param redisSkuName string = 'Standard'

@description('Capacidad Redis. C0=0 (250MB), C1=1 (1GB), C2=2 (2.5GB). Default 1. Para staging 0 alcanza.')
@minValue(0)
@maxValue(6)
param redisCapacity int = 1

@description('Tags propagados a todos los recursos.')
param tags object = {
  project: 'miproposito-2.0'
  client: 'UCSP'
  vendor: 'BlueNose'
  environment: 'staging'
}

// ----- Módulos -----

module network 'modules/network.bicep' = {
  name: 'network'
  params: {
    namePrefix: namePrefix
    location: location
    tags: tags
  }
}

module acr 'modules/acr.bicep' = {
  name: 'acr'
  params: {
    namePrefix: namePrefix
    location: location
    tags: tags
  }
}

module aks 'modules/aks.bicep' = {
  name: 'aks'
  params: {
    namePrefix: namePrefix
    location: location
    aksSubnetId: network.outputs.aksSubnetId
    acrId: acr.outputs.acrId
    nodeVmSize: aksNodeVmSize
    nodeCount: aksNodeCount
    tags: tags
  }
}

module sql 'modules/sql.bicep' = if (enableAzureSql) {
  name: 'sql'
  params: {
    namePrefix: namePrefix
    location: location
    sqlPeSubnetId: network.outputs.sqlPeSubnetId
    vnetId: network.outputs.vnetId
    sqlAdminLogin: sqlAdminLogin
    sqlAdminPassword: sqlAdminPassword
    tags: tags
  }
}

module keyVault 'modules/keyvault.bicep' = {
  name: 'keyvault'
  params: {
    namePrefix: namePrefix
    location: location
    adminObjectId: adminObjectId
    secretsProviderObjectId: aks.outputs.secretsProviderObjectId
    tags: tags
  }
}

module storage 'modules/storage.bicep' = {
  name: 'storage'
  params: {
    namePrefix: namePrefix
    location: location
    tags: tags
  }
}

module redis 'modules/redis.bicep' = if (enableAzureRedis) {
  name: 'redis'
  params: {
    namePrefix: namePrefix
    location: location
    skuName: redisSkuName
    capacity: redisCapacity
    tags: tags
  }
}

// Public IP solo si el App Gateway está habilitado (para no pagar por
// una IP que nadie usa).
resource appGwPublicIp 'Microsoft.Network/publicIPAddresses@2024-01-01' = if (enableAppGateway) {
  name: 'pip-${namePrefix}-agw'
  location: location
  tags: tags
  sku: { name: 'Standard' }
  properties: {
    publicIPAllocationMethod: 'Static'
    dnsSettings: {
      domainNameLabel: '${namePrefix}-agw'
    }
  }
}

module appGateway 'modules/app-gateway.bicep' = if (enableAppGateway) {
  name: 'app-gateway'
  params: {
    namePrefix: namePrefix
    location: location
    wafSubnetId: network.outputs.wafSubnetId
    publicIpAddressId: enableAppGateway ? appGwPublicIp.id : ''
    backendHostname: 'api.miproposito.ucsp.edu.pe' // configurado en NGINX Ingress
    tags: tags
  }
}

// ----- Outputs (los consume install.ps1 + Azure DevOps Variable Group) -----
output resourceGroupName string = resourceGroup().name
output location string = location
output namePrefix string = namePrefix

// AKS
output aksClusterName string = aks.outputs.clusterName
output aksKubeletClientId string = aks.outputs.kubeletIdentityClientId
output aksSecretsProviderClientId string = aks.outputs.secretsProviderClientId

// SQL — vacío cuando enableAzureSql=false (el helm chart usa el SQL in-cluster)
output sqlServerFqdn string = enableAzureSql ? sql.outputs.sqlServerFqdn : ''
output sqlServerName string = enableAzureSql ? sql.outputs.sqlServerName : ''
output databaseNames array = enableAzureSql ? sql.outputs.databaseNames : ['db_users', 'db_exams', 'db_keys', 'db_satisfaction']
output azureSqlEnabled bool = enableAzureSql

// Key Vault
output keyVaultName string = keyVault.outputs.keyVaultName
output keyVaultUri string = keyVault.outputs.keyVaultUri

// ACR
output acrLoginServer string = acr.outputs.acrLoginServer

// Redis — vacío cuando enableAzureRedis=false (el helm chart usa el Redis in-cluster)
output redisHost string = enableAzureRedis ? redis.outputs.redisHost : ''
output redisSslPort int = enableAzureRedis ? redis.outputs.redisSslPort : 0
output azureRedisEnabled bool = enableAzureRedis

// Storage
output storageAccountName string = storage.outputs.storageAccountName

// Workload identity / tenant info (para values.production.yaml de Helm)
output tenantId string = subscription().tenantId
