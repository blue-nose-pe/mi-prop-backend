// VNet `vnet-ucsp-aks` con 4 subnets segregadas + Network Security Groups,
// replicando el diagrama del cliente:
//   ┌────────────┐      ┌─────────────────────────────────────┐
//   │ WAF subnet │ ──── │ vnet-ucsp-aks                        │
//   │ (App Gw)   │      │  ├── lb-subnet      (Load Balancers) │
//   └────────────┘      │  ├── aks-subnet     (AKS pods)       │
//                       │  └── sql-pe-subnet  (SQL endpoint)   │
//                       └─────────────────────────────────────┘
//
// Espacios CIDR generosos para no tener que migrar después:
//   waf-subnet     10.10.0.0/24    (256 IPs — App Gateway WAF v2)
//   lb-subnet      10.10.1.0/24    (256 IPs — Internal Load Balancers)
//   aks-subnet     10.10.16.0/20   (4096 IPs — pods + nodos)
//   sql-pe-subnet  10.10.32.0/24   (256 IPs — Private Endpoints)

@description('Prefix usado en todos los recursos.')
param namePrefix string

@description('Región Azure.')
param location string

@description('Tags a propagar a todos los recursos.')
param tags object = {}

// ----------------------------------------------------------------------
// VNet
// ----------------------------------------------------------------------
resource vnet 'Microsoft.Network/virtualNetworks@2024-01-01' = {
  name: 'vnet-${namePrefix}-aks'
  location: location
  tags: tags
  properties: {
    addressSpace: {
      addressPrefixes: ['10.10.0.0/16']
    }
    subnets: [
      {
        name: 'waf-subnet'
        properties: {
          addressPrefix: '10.10.0.0/24'
          networkSecurityGroup: { id: nsgWaf.id }
        }
      }
      {
        name: 'lb-subnet'
        properties: {
          addressPrefix: '10.10.1.0/24'
          networkSecurityGroup: { id: nsgLb.id }
        }
      }
      {
        name: 'aks-subnet'
        properties: {
          addressPrefix: '10.10.16.0/20'
          networkSecurityGroup: { id: nsgAks.id }
        }
      }
      {
        name: 'sql-pe-subnet'
        properties: {
          addressPrefix: '10.10.32.0/24'
          networkSecurityGroup: { id: nsgSqlPe.id }
          // Required for Private Endpoint: disable network policies.
          privateEndpointNetworkPolicies: 'Disabled'
        }
      }
    ]
  }
}

// ----------------------------------------------------------------------
// Network Security Groups
// ----------------------------------------------------------------------

// WAF subnet: tráfico entrante HTTPS desde Internet, infra Azure.
resource nsgWaf 'Microsoft.Network/networkSecurityGroups@2024-01-01' = {
  name: 'nsg-${namePrefix}-waf'
  location: location
  tags: tags
  properties: {
    securityRules: [
      {
        name: 'AllowHttpsInbound'
        properties: {
          priority: 100
          direction: 'Inbound'
          access: 'Allow'
          protocol: 'Tcp'
          sourceAddressPrefix: 'Internet'
          sourcePortRange: '*'
          destinationAddressPrefix: '*'
          destinationPortRange: '443'
        }
      }
      {
        // Application Gateway necesita los puertos 65200-65535 desde
        // GatewayManager para health probes (Microsoft maneja este service tag).
        name: 'AllowGatewayManager'
        properties: {
          priority: 110
          direction: 'Inbound'
          access: 'Allow'
          protocol: '*'
          sourceAddressPrefix: 'GatewayManager'
          sourcePortRange: '*'
          destinationAddressPrefix: '*'
          destinationPortRange: '65200-65535'
        }
      }
    ]
  }
}

// LB subnet: tráfico interno, sin reglas extra.
resource nsgLb 'Microsoft.Network/networkSecurityGroups@2024-01-01' = {
  name: 'nsg-${namePrefix}-lb'
  location: location
  tags: tags
}

// AKS subnet: AKS gestiona sus propias reglas mediante CNI; no agregamos
// restricciones agresivas para no romper tráfico de control.
resource nsgAks 'Microsoft.Network/networkSecurityGroups@2024-01-01' = {
  name: 'nsg-${namePrefix}-aks'
  location: location
  tags: tags
}

// SQL Private Endpoint subnet: solo tráfico desde la AKS subnet.
resource nsgSqlPe 'Microsoft.Network/networkSecurityGroups@2024-01-01' = {
  name: 'nsg-${namePrefix}-sql-pe'
  location: location
  tags: tags
  properties: {
    securityRules: [
      {
        name: 'AllowSqlFromAks'
        properties: {
          priority: 100
          direction: 'Inbound'
          access: 'Allow'
          protocol: 'Tcp'
          sourceAddressPrefix: '10.10.16.0/20' // aks-subnet
          sourcePortRange: '*'
          destinationAddressPrefix: '*'
          destinationPortRange: '1433'
        }
      }
      {
        name: 'DenyAllOtherInbound'
        properties: {
          priority: 4096
          direction: 'Inbound'
          access: 'Deny'
          protocol: '*'
          sourceAddressPrefix: '*'
          sourcePortRange: '*'
          destinationAddressPrefix: '*'
          destinationPortRange: '*'
        }
      }
    ]
  }
}

// ----------------------------------------------------------------------
// Outputs (los consume main.bicep para conectar AKS, SQL endpoint, etc.)
// ----------------------------------------------------------------------
output vnetId string = vnet.id
output vnetName string = vnet.name
output wafSubnetId string = '${vnet.id}/subnets/waf-subnet'
output lbSubnetId string = '${vnet.id}/subnets/lb-subnet'
output aksSubnetId string = '${vnet.id}/subnets/aks-subnet'
output sqlPeSubnetId string = '${vnet.id}/subnets/sql-pe-subnet'
