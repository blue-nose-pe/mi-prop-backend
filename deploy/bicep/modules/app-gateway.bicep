// Application Gateway + WAF v2 — opcional. Default OFF.
//
// Costo aproximado: ~$200-300 USD/mes (medium tier). Por eso lo dejamos
// deshabilitado por default — provisionalmente UCSP usa el gateway custom
// (Go) en AKS expuesto via NGINX Ingress + Public IP. Cuando UCSP esté
// listo a pagar, activar `enableWaf=true` en main.bicep.
//
// Cuando se active, el flujo es:
//   Internet → App Gateway (con reglas WAF OWASP) → AKS Ingress NGINX →
//   gateway pod → microservicios gRPC

@description('Prefix usado en todos los recursos.')
param namePrefix string

@description('Región Azure.')
param location string

@description('Subnet `waf-subnet` donde vive el App Gateway.')
param wafSubnetId string

@description('IP pública del Application Gateway.')
param publicIpAddressId string

@description('Hostname del backend (NGINX Ingress en AKS).')
param backendHostname string

@description('Tags.')
param tags object = {}

resource appGw 'Microsoft.Network/applicationGateways@2024-01-01' = {
  name: 'agw-${namePrefix}'
  location: location
  tags: tags
  properties: {
    sku: {
      name: 'WAF_v2'
      tier: 'WAF_v2'
      capacity: 2
    }
    gatewayIPConfigurations: [
      {
        name: 'gw-ip-config'
        properties: { subnet: { id: wafSubnetId } }
      }
    ]
    frontendIPConfigurations: [
      {
        name: 'public-frontend'
        properties: { publicIPAddress: { id: publicIpAddressId } }
      }
    ]
    frontendPorts: [
      { name: 'port-443', properties: { port: 443 } }
    ]
    backendAddressPools: [
      {
        name: 'aks-backend'
        properties: { backendAddresses: [ { fqdn: backendHostname } ] }
      }
    ]
    backendHttpSettingsCollection: [
      {
        name: 'aks-https-settings'
        properties: {
          port: 443
          protocol: 'Https'
          cookieBasedAffinity: 'Disabled'
          requestTimeout: 30
          pickHostNameFromBackendAddress: true
        }
      }
    ]
    httpListeners: [
      {
        name: 'https-listener'
        properties: {
          frontendIPConfiguration: { id: resourceId('Microsoft.Network/applicationGateways/frontendIPConfigurations', 'agw-${namePrefix}', 'public-frontend') }
          frontendPort: { id: resourceId('Microsoft.Network/applicationGateways/frontendPorts', 'agw-${namePrefix}', 'port-443') }
          protocol: 'Https'
        }
      }
    ]
    requestRoutingRules: [
      {
        name: 'main-rule'
        properties: {
          ruleType: 'Basic'
          priority: 100
          httpListener: { id: resourceId('Microsoft.Network/applicationGateways/httpListeners', 'agw-${namePrefix}', 'https-listener') }
          backendAddressPool: { id: resourceId('Microsoft.Network/applicationGateways/backendAddressPools', 'agw-${namePrefix}', 'aks-backend') }
          backendHttpSettings: { id: resourceId('Microsoft.Network/applicationGateways/backendHttpSettingsCollection', 'agw-${namePrefix}', 'aks-https-settings') }
        }
      }
    ]
    webApplicationFirewallConfiguration: {
      enabled: true
      firewallMode: 'Prevention'
      ruleSetType: 'OWASP'
      ruleSetVersion: '3.2'
    }
  }
}

output appGwId string = appGw.id
