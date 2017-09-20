package integration

import (
	"fmt"

	"github.com/feedhenry/mcp-standalone/pkg/mobile"
	"github.com/pkg/errors"
)

// MobileService holds the business logic for dealing with the mobile services and integrations with those services
type MobileService struct {
	namespace string
}

//NewMobileSevice reutrns  a new mobile server
func NewMobileSevice(ns string) *MobileService {
	return &MobileService{
		namespace: ns,
	}
}

//FindByNames will return all services with a name that matches the provided name
func (ms *MobileService) FindByNames(names []string, serviceCruder mobile.ServiceCruder) ([]*mobile.Service, error) {
	svc, err := serviceCruder.List(filterServices(names))
	if err != nil {
		return nil, errors.Wrap(err, "Attempting to discover mobile services.")
	}
	return svc, nil
}

// TODO move to the secret data read when discovering the services
var capabilities = map[string]map[string][]string{
	"fh-sync-server": map[string][]string{
		"capabilities": {"data storage, data syncronisation"},
		"integrations": {"keycloak"},
	},
	"keycloak": map[string][]string{
		"capabilities": {"authentication, authorisation"},
		"integrations": {"fh-sync"},
	},
	"custom": map[string][]string{
		"capabilities": {""},
		"integrations": {""},
	},
}

// DiscoverMobileServices will discover mobile services configured in the current namespace
func (ms *MobileService) DiscoverMobileServices(serviceCruder mobile.ServiceCruder, authChecker mobile.AuthChecker) ([]*mobile.Service, error) {
	svc, err := serviceCruder.List(filterServices(mobile.ServiceTypes))
	if err != nil {
		return nil, errors.Wrap(err, "Attempting to discover mobile services.")
	}
	for _, s := range svc {
		s.Capabilities = capabilities[s.Name]
		//non external services are part of the current namespace //TODO maybe should be added to the apbs
		if s.External == false {
			if s.Namespace == "" {
				s.Namespace = ms.namespace
			}
			s.Writeable = true
		}
		if s.External {
			perm, err := authChecker.Check("deployments", s.Namespace)
			if err != nil {
				return nil, errors.Wrap(err, "error checking access permissions")
			}
			s.Writeable = perm
		}
	}
	return svc, nil
}

// ReadMobileServiceAndIntegrations read service and any available service it can integrate with
func (ms *MobileService) ReadMobileServiceAndIntegrations(serviceCruder mobile.ServiceCruder, authChecker mobile.AuthChecker, name string) (*mobile.Service, error) {
	svc, err := serviceCruder.Read(name)
	if err != nil {
		return nil, errors.Wrap(err, "attempting to discover mobile services.")
	}
	svc.Capabilities = capabilities[svc.Type]
	if svc.Capabilities != nil {
		fmt.Println("checking for capabilties of", svc.Type)
		integrations := svc.Capabilities["integrations"]
		for _, v := range integrations {
			fmt.Println("checking for instance of", v)
			isvs, err := serviceCruder.List(filterServices([]string{v}))
			if err != nil {
				return nil, errors.Wrap(err, "failed attempting to discover mobile services.")
			}
			if len(isvs) > 0 {
				fmt.Println("found", v)
				is := isvs[0]
				enabled := svc.Labels[is.Name] == "true"
				svc.Integrations[v] = &mobile.ServiceIntegration{
					ComponentSecret: svc.ID,
					Component:       svc.Type,
					Namespace:       ms.namespace,
					Service:         is.ID,
					Enabled:         enabled,
				}
			}
		}
	} else {
		fmt.Println("no capabilities found for", svc.Type)
	}
	svc.Writeable = true
	if svc.External {
		perm, err := authChecker.Check("deployments", svc.Namespace)
		if err != nil {
			return nil, errors.Wrap(err, "error checking access permissions")
		}
		svc.Writeable = perm
	}
	return svc, nil
}

func filterServices(serviceTypes []string) func(att mobile.Attributer) bool {
	return func(att mobile.Attributer) bool {
		for _, sn := range serviceTypes {
			if sn == att.GetType() {
				return true
			}
		}
		return false
	}
}

//NOTE do we want to have a usecae for mounting the secrets to allow for logic around services and secrets in different namespaces?

//MountSecretForComponent will mount secret into component, returning any errors
func (ms *MobileService) MountSecretForComponent(svcCruder mobile.ServiceCruder, mounter mobile.VolumeMounter, clientService, serviceSecret string) error {
	//check secret exists and store for later update
	service, err := svcCruder.Read(serviceSecret)
	if err != nil {
		return errors.Wrap(err, "failed to find secret: '"+serviceSecret+"'")
	}

	cServiceList, err := svcCruder.List(filterServices([]string{clientService}))
	if err != nil || len(cServiceList) == 0 {
		return errors.New("failed to find secret for client service: '" + clientService + "'")
	}
	cService := cServiceList[0]

	err = mounter.Mount(service, cService)
	if err != nil {
		return errors.Wrap(err, "failed to mount secret '"+serviceSecret+"' into service '"+clientService+"'")
	}

	clientServiceID := cService.ID

	//update secret with integration enabled
	enabled := map[string]string{service.Type: "true"}
	if err := svcCruder.UpdateEnabledIntegrations(clientServiceID, enabled); err != nil {
		return errors.Wrap(err, "failed to update enabled services after mounting secret")
	}

	return nil
}

//UnmountSecretInComponent will unmount secret from component, so it can be no longer use serviceName, returning any errors
func (ms *MobileService) UnmountSecretInComponent(svcCruder mobile.ServiceCruder, unmounter mobile.VolumeUnmounter, clientService, serviceSecret string) error {
	//check secret exists and store for later update
	service, err := svcCruder.Read(serviceSecret)
	if err != nil {
		return errors.Wrap(err, "failed to find secret: '"+serviceSecret+"'")
	}

	//find the clientService secret name
	css, err := svcCruder.List(filterServices([]string{clientService}))
	if err != nil || len(css) == 0 {
		return errors.New("failed to find secret for client service: '" + clientService + "'")
	}
	cService := css[0]

	err = unmounter.Unmount(service, cService)
	if err != nil {
		return errors.Wrap(err, "failed to unmount secret '"+serviceSecret+"' from component '"+clientService+"'")
	}

	clientServiceId := cService.ID

	//update secret with integration enabled
	disabled := map[string]string{service.Type: "false"}
	if err := svcCruder.UpdateEnabledIntegrations(clientServiceId, disabled); err != nil {
		return errors.Wrap(err, "failed to update enabled services after unmounting secret")
	}

	return nil
}
