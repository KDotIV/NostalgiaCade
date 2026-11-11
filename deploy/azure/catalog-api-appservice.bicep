targetScope = 'resourceGroup'

@description('Stable API app name. Reuse this name for every deployment.')
param appName string
@description('Existing Windows App Service plan in this resource group. No plan is created or resized.')
param planName string
@description('Region of the existing App Service plan.')
param location string = resourceGroup().location
@minLength(1)
@description('Administrator public IP ranges in CIDR notation.')
param adminCidrs array

resource plan 'Microsoft.Web/serverfarms@2024-11-01' existing = {
  name: planName
}

resource api 'Microsoft.Web/sites@2024-11-01' = {
  name: appName
  location: location
  kind: 'app'
  tags: {
    application: 'NCade'
    component: 'catalog-api'
  }
  properties: {
    serverFarmId: plan.id
    httpsOnly: true
    clientAffinityEnabled: false
    publicNetworkAccess: 'Enabled'
    siteConfig: {
      alwaysOn: true
      use32BitWorkerProcess: false
      netFrameworkVersion: ''
      managedPipelineMode: 'Integrated'
      http20Enabled: true
      minTlsVersion: '1.2'
      scmMinTlsVersion: '1.2'
      ftpsState: 'Disabled'
      remoteDebuggingEnabled: false
      healthCheckPath: '/health/ready'
      ipSecurityRestrictionsDefaultAction: 'Deny'
      ipSecurityRestrictions: [for (cidr, index) in adminCidrs: {
        name: 'admin-${index}'
        action: 'Allow'
        priority: 100 + index
        ipAddress: cidr
      }]
      scmIpSecurityRestrictionsUseMain: true
    }
  }
}

resource scmCredentials 'Microsoft.Web/sites/basicPublishingCredentialsPolicies@2024-11-01' = {
  parent: api
  name: 'scm'
  properties: { allow: false }
}

resource ftpCredentials 'Microsoft.Web/sites/basicPublishingCredentialsPolicies@2024-11-01' = {
  parent: api
  name: 'ftp'
  properties: { allow: false }
}

output appResourceId string = api.id
output baseUrl string = 'https://${api.properties.defaultHostName}'
