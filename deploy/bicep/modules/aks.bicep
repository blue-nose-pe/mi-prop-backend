// AKS PROD: cluster con system-assigned identity, addon Key Vault Secrets
// Provider, Workload Identity habilitado, integrado a la subred
// `aks-subnet` de la VNet.
//
// Tier: "Free" en lo provisional (gratis con SLA del 99.5% para clusters
// chicos). El cliente puede subirlo a "Standard" cuando lo necesite (eso
// agrega ~$73/mes pero da 99.95% SLA).

@description('Prefix usado en todos los recursos.')
param namePrefix string

@description('Región Azure.')
param location string

@description('Subnet para los nodos del AKS.')
param aksSubnetId string

@description('ACR resource ID para enchufar AcrPull al kubelet.')
param acrId string

@description('Tags.')
param tags object = {}

@description('SKU de la VM de los nodos. Default Standard_D2s_v3 (2vCPU/8GB).')
param nodeVmSize string = 'Standard_D2s_v3'

@description('Cantidad de nodos. 3 es lo recomendado para HA.')
param nodeCount int = 3

@description('Versión de Kubernetes. Dejar vacío para usar la default que Azure recomienda.')
param kubernetesVersion string = ''

resource aks 'Microsoft.ContainerService/managedClusters@2024-09-01' = {
  name: 'aks-${namePrefix}'
  location: location
  tags: tags
  identity: {
    type: 'SystemAssigned'
  }
  sku: {
    name: 'Base'
    tier: 'Free'
  }
  properties: {
    dnsPrefix: '${namePrefix}-aks'
    kubernetesVersion: empty(kubernetesVersion) ? null : kubernetesVersion
    enableRBAC: true
    oidcIssuerProfile: { enabled: true }     // habilita workload identity
    securityProfile: {
      workloadIdentity: { enabled: true }
    }
    addonProfiles: {
      // Secrets Store CSI driver con provider Azure Key Vault — esto es
      // lo que monta /mnt/secrets en cada pod y resuelve el SecretProviderClass.
      azureKeyvaultSecretsProvider: {
        enabled: true
        config: {
          enableSecretRotation: 'true'
          rotationPollInterval: '5m'
        }
      }
      azurepolicy: { enabled: false }
    }
    agentPoolProfiles: [
      {
        name: 'system'
        mode: 'System'
        count: nodeCount
        vmSize: nodeVmSize
        osType: 'Linux'
        osDiskSizeGB: 64
        type: 'VirtualMachineScaleSets'
        vnetSubnetID: aksSubnetId
        // Auto-scaling on por default — útil para que UCSP no tenga que
        // pensar en escalar manualmente.
        enableAutoScaling: true
        minCount: nodeCount
        maxCount: nodeCount + 3
      }
    ]
    networkProfile: {
      networkPlugin: 'azure'
      networkPolicy: 'azure'
      loadBalancerSku: 'standard'
      serviceCidr: '10.20.0.0/16'   // services internos del cluster
      dnsServiceIP: '10.20.0.10'
    }
    apiServerAccessProfile: {
      enablePrivateCluster: false   // público con autenticación AAD; cambiar a true si UCSP exige aislar el control plane
    }
  }
}

// AcrPull: la kubelet identity necesita poder pull-ear imágenes del ACR.
// Usamos el role definition built-in.
var acrPullRoleDefId = subscriptionResourceId('Microsoft.Authorization/roleDefinitions', '7f951dda-4ed3-4680-a7ca-43fe172d538d') // AcrPull

resource aksToAcrRole 'Microsoft.Authorization/roleAssignments@2022-04-01' = {
  name: guid(aks.id, acrId, 'AcrPull')
  scope: resourceGroup()  // a nivel RG es suficiente — el ACR vive acá
  properties: {
    principalId: aks.properties.identityProfile.kubeletidentity.objectId
    principalType: 'ServicePrincipal'
    roleDefinitionId: acrPullRoleDefId
  }
}

// ----------------------------------------------------------------------
// Outputs
// ----------------------------------------------------------------------
output clusterName string = aks.name
output clusterId string = aks.id
output kubeletIdentityClientId string = aks.properties.identityProfile.kubeletidentity.clientId
output kubeletIdentityObjectId string = aks.properties.identityProfile.kubeletidentity.objectId
output secretsProviderClientId string = aks.properties.addonProfiles.azureKeyvaultSecretsProvider.identity.clientId
// Object ID (≠ Client ID) — necesario para roleAssignment al Key Vault.
// Sin este, el CSI driver del SCC falla con AADSTS70025 al montar secretos.
output secretsProviderObjectId string = aks.properties.addonProfiles.azureKeyvaultSecretsProvider.identity.objectId

// Resource group "MC_*" donde AKS crea sus recursos managed (VMSS, MIs del addon).
// install.ps1 lo necesita para crear las federated identity credentials.
output nodeResourceGroup string = aks.properties.nodeResourceGroup
output oidcIssuerUrl string = aks.properties.oidcIssuerProfile.issuerURL
