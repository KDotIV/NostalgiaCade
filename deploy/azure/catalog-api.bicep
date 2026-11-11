targetScope = 'resourceGroup'

param appName string = 'ncade-catalog-api'
param location string = resourceGroup().location
@description('Existing Azure Container Apps environment in this region.')
param environmentId string
@description('Immutable image reference, preferably registry/repository@sha256:digest.')
param image string
param registryServer string
@description('Existing user-assigned identity with AcrPull and Key Vault Secrets User on the appropriate resources.')
param identityResourceId string
@description('Key Vault URI for the PostgreSQL DSN, using sslmode=verify-full and a runtime-only database role.')
param postgresSecretUrl string
@description('Key Vault URI for the admin token. Provision a distinct token for each environment.')
param adminTokenSecretUrl string
param adminSubject string = 'Sengoku-Admin'
@minLength(1)
@description('Administrator public IP ranges in CIDR notation. No default public allow-all rule.')
param adminCidrs array
@minValue(0)
param minReplicas int = 0
@minValue(1)
param maxReplicas int = 2

resource api 'Microsoft.App/containerApps@2025-01-01' = {
  name: appName
  location: location
  identity: {
    type: 'UserAssigned'
    userAssignedIdentities: {
      '${identityResourceId}': {}
    }
  }
  properties: {
    environmentId: environmentId
    configuration: {
      activeRevisionsMode: 'Single'
      ingress: {
        external: true
        allowInsecure: false
        targetPort: 8080
        transport: 'auto'
        ipSecurityRestrictions: [for (cidr,index) in adminCidrs: {
          name: 'admin-${index}'
          action: 'Allow'
          ipAddressRange: cidr
        }]
      }
      registries: [{ server: registryServer, identity: identityResourceId }]
      secrets: [
        { name: 'postgres-dsn', keyVaultUrl: postgresSecretUrl, identity: identityResourceId }
        { name: 'admin-token', keyVaultUrl: adminTokenSecretUrl, identity: identityResourceId }
      ]
    }
    template: {
      terminationGracePeriodSeconds: 30
      containers: [{
        name: 'api'
        image: image
        resources: { cpu: json('0.25'), memory: '0.5Gi' }
        env: [
          { name: 'NCADE_API_LISTEN', value: '0.0.0.0:8080' }
          { name: 'NCADE_API_BEHIND_TLS_PROXY', value: 'true' }
          { name: 'NCADE_POSTGRES_DSN', secretRef: 'postgres-dsn' }
          { name: 'NCADE_ADMIN_TOKEN', secretRef: 'admin-token' }
          { name: 'NCADE_ADMIN_SUBJECT', value: adminSubject }
        ]
        probes: [
          { type: 'Startup', httpGet: { path: '/health/live', port: 8080 }, periodSeconds: 2, timeoutSeconds: 2, failureThreshold: 30 }
          { type: 'Liveness', httpGet: { path: '/health/live', port: 8080 }, periodSeconds: 10, timeoutSeconds: 2, failureThreshold: 3 }
          { type: 'Readiness', httpGet: { path: '/health/ready', port: 8080 }, periodSeconds: 10, timeoutSeconds: 3, failureThreshold: 3 }
        ]
      }]
      scale: {
        minReplicas: minReplicas
        maxReplicas: maxReplicas
        rules: [{ name: 'http', http: { metadata: { concurrentRequests: '20' } } }]
      }
    }
  }
}

output baseUrl string = 'https://${api.properties.configuration.ingress.fqdn}'
