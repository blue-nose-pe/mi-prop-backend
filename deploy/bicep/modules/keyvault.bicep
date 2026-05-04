// Azure Key Vault con RBAC habilitado.
// El admin que despliega obtiene "Key Vault Administrator" automáticamente.
// La identidad del addon Secrets Provider del AKS obtiene "Key Vault
// Secrets User" para poder LEER (no escribir) los secretos.

@description('Prefix usado en todos los recursos.')
param namePrefix string

@description('Región Azure.')
param location string

@description('Object ID del usuario que despliega — recibirá Key Vault Administrator.')
param adminObjectId string

@description('Object ID del Service Principal del addon Secrets Provider del AKS.')
param secretsProviderObjectId string

@description('Tags.')
param tags object = {}

resource kv 'Microsoft.KeyVault/vaults@2024-04-01-preview' = {
  name: '${namePrefix}-kv'
  location: location
  tags: tags
  properties: {
    sku: { family: 'A', name: 'standard' }
    tenantId: subscription().tenantId
    enableRbacAuthorization: true
    enableSoftDelete: true
    softDeleteRetentionInDays: 90
    enabledForDeployment: false
    enabledForTemplateDeployment: false
    enabledForDiskEncryption: false
    publicNetworkAccess: 'Enabled'   // por simplicidad provisional; producción real → Disabled + Private Endpoint
  }
}

// Built-in role IDs.
var keyVaultAdminRoleId = subscriptionResourceId('Microsoft.Authorization/roleDefinitions', '00482a5a-887f-4fb3-b363-3b7fe8e74483')
var keyVaultSecretsUserId = subscriptionResourceId('Microsoft.Authorization/roleDefinitions', '4633458b-17de-408a-b874-0445c86b69e6')

resource adminAssignment 'Microsoft.Authorization/roleAssignments@2022-04-01' = {
  scope: kv
  name: guid(kv.id, adminObjectId, 'KeyVaultAdministrator')
  properties: {
    principalId: adminObjectId
    principalType: 'User'
    roleDefinitionId: keyVaultAdminRoleId
  }
}

resource secretsProviderAssignment 'Microsoft.Authorization/roleAssignments@2022-04-01' = {
  scope: kv
  name: guid(kv.id, secretsProviderObjectId, 'KeyVaultSecretsUser')
  properties: {
    principalId: secretsProviderObjectId
    principalType: 'ServicePrincipal'
    roleDefinitionId: keyVaultSecretsUserId
  }
}

output keyVaultName string = kv.name
output keyVaultUri string = kv.properties.vaultUri
