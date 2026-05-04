// Azure Container Registry. Tier Standard (más barato que Premium y
// suficiente para Mi Propósito 2.0; Premium se necesita solo para
// geo-replication o private endpoints).

@description('Prefix usado en todos los recursos.')
param namePrefix string

@description('Región Azure.')
param location string

@description('Tags.')
param tags object = {}

resource acr 'Microsoft.ContainerRegistry/registries@2023-11-01-preview' = {
  // ACR exige nombres alfanuméricos sin guiones; el prefix queda como '<namePrefix>acr'.
  name: '${namePrefix}acr'
  location: location
  tags: tags
  sku: { name: 'Standard' }
  properties: {
    adminUserEnabled: false        // recomendación de seguridad: usar AAD
    publicNetworkAccess: 'Enabled' // mismo razonamiento que KV — provisional público
  }
}

output acrId string = acr.id
output acrName string = acr.name
output acrLoginServer string = acr.properties.loginServer
