// Azure Storage Account: almacenamiento de exports XLSX, attachments
// futuros y lo que el cliente quiera persistir como blobs.
//
// Tier: Standard_LRS (3 réplicas locales, lo más barato). Si UCSP exige
// disaster recovery → cambiar a GRS (Geo-redundant) en values.production.

@description('Prefix usado en todos los recursos.')
param namePrefix string

@description('Región Azure.')
param location string

@description('Tags.')
param tags object = {}

resource storage 'Microsoft.Storage/storageAccounts@2024-01-01' = {
  // Storage Account exige máx 24 chars, alfanumérico minúsculo.
  name: take(toLower(replace('${namePrefix}storage', '-', '')), 24)
  location: location
  tags: tags
  sku: { name: 'Standard_LRS' }
  kind: 'StorageV2'
  properties: {
    accessTier: 'Hot'
    allowBlobPublicAccess: false
    minimumTlsVersion: 'TLS1_2'
    supportsHttpsTrafficOnly: true
  }
}

resource exportsContainer 'Microsoft.Storage/storageAccounts/blobServices/containers@2024-01-01' = {
  name: '${storage.name}/default/exports'
  properties: {
    publicAccess: 'None'
  }
}

output storageAccountName string = storage.name
output storageBlobEndpoint string = storage.properties.primaryEndpoints.blob
