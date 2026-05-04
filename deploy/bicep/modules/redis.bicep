// Azure Cache for Redis. Lo usan:
//   * hubspot-service: cola asynq de jobs
//   * gateway: rate-limiter por IP
//   * analytics-service: cache de dashboards (TTL 5 min)
//   * permsclient en cada microservicio: cache de permisos (TTL 30s)
//
// SKU mínimo: Basic C0 (250MB, sin SLA). Para producción real con SLA
// 99.9% → cambiar a Standard C1 (mismo tamaño, +$30/mes).

@description('Prefix usado en todos los recursos.')
param namePrefix string

@description('Región Azure.')
param location string

@description('SKU. Para staging/dev: Basic. Producción: Standard o Premium.')
@allowed(['Basic', 'Standard', 'Premium'])
param skuName string = 'Standard'

@description('Capacidad. C0=0 (250MB), C1=1 (1GB), C2=2 (2.5GB)...')
param capacity int = 1

@description('Tags.')
param tags object = {}

resource redis 'Microsoft.Cache/redis@2024-04-01-preview' = {
  name: '${namePrefix}-redis'
  location: location
  tags: tags
  properties: {
    sku: {
      name: skuName
      family: skuName == 'Premium' ? 'P' : 'C'
      capacity: capacity
    }
    enableNonSslPort: false
    minimumTlsVersion: '1.2'
    publicNetworkAccess: 'Enabled' // provisional — cambiar a Disabled cuando se agregue Private Endpoint
  }
}

output redisHost string = redis.properties.hostName
output redisSslPort int = redis.properties.sslPort
