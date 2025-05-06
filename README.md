# 10-vm-with-public-exposed-ip-address

Repository test per il progetto "10-vm-with-public-exposed-ip-address" assegnato per il corso di Cloud Programming.  
Questo progetto estende il progetto CrownLabs per abilitare la creazione di indirizzi IP pubblici associati a VM o container, rendendo i servizi backend accessibili da Internet.

---

## **Obiettivo**
CrownLabs è stato progettato per fornire VM/container per laboratori universitari. Tuttavia, questo progetto mira a:
- Abilitare la creazione di un indirizzo IP pubblico (simile a FloatingIPs in OpenStack) associato a una VM/container.
- Consentire agli utenti di configurare autonomamente questa funzionalità tramite un'interfaccia grafica.
- Ottimizzare l'uso degli indirizzi IP pubblici creando un mapping tra porte TCP/UDP pubbliche e porte interne.

Esempio di mapping:
- `PublicIP:2222 --> VMIP:22` - Descrizione: SSH
- `PublicIP:8080 --> VMIP:80` - Descrizione: Web server interno

---

## **Componenti utilizzati**
Il progetto utilizza i seguenti componenti:
- **MetalLB**: Usato come LoadBalancer di indirizzi IP, per gestire l'assegnazione degli indirizzi IP pubblici alle VM che ne faranno richiesta.
- **KubeVirt**: Per creare e gestire Virtual Machines (VM) all'interno del cluster Kubernetes.
- **Kind**: Per creare un cluster Kubernetes locale.
- **Cilium**: Per gestire la rete del cluster Kubernetes.
- **Kubebuilder**: Per creare un controller personalizzato che automatizza la gestione delle risorse.
- **Go**: Per sviluppare il controller

---

## **Installazione e configurazione**

### **1. Prerequisiti**
Assicurati di avere installati i seguenti strumenti:
- **Go** (versione 1.20 o successiva): [https://go.dev/dl/](https://go.dev/dl/)
- **Kind**: [https://kind.sigs.k8s.io/](https://kind.sigs.k8s.io/)
- **kubectl**: [https://kubernetes.io/docs/tasks/tools/](https://kubernetes.io/docs/tasks/tools/)
- **Helm** (per installare Cilium e altri servizi): [https://helm.sh/](https://helm.sh/)
- **Docker**: Per eseguire i container del cluster Kind.

Verifica che gli strumenti siano installati:
```bash
go version
kind version
kubectl version --client
docker --version
```
### **2. Setup basic Cluster**

#### Run Kind Cluster

   ```bash
   kind create cluster --name argocddemo --config kind-config.yml
   ```

#### Install a CNI

   ```bash
   cilium install --wait
   ```

### **3. Add LoadBalancer for IP Configuration**
   ```bash
      kubectl create namespace metallb-system
      helm install metallb metallb/metallb -n metallb-system -f metallb-config.yaml
   ```

### **4. Deploy Kubevirt for VM operations**
Segui in ordine questa serie di comandi.
Verifica di avere "curl" installato prima di procedere.
```bash
### Point at latest release
$ export RELEASE=$(curl https://storage.googleapis.com/kubevirt-prow/release/kubevirt/kubevirt/stable.txt)
```
Fai un check che tutto sia ok attraverso
```bash
echo $RELEASE
### response ex. v0.19.0
```
Dopodichè procediamo con l'installazione degli operatore e della CR che farà da trigger per l'operator.
```bash
### Deploy the KubeVirt operator
kubectl apply -f https://github.com/kubevirt/kubevirt/releases/download/${RELEASE}/kubevirt-operator.yaml
```
```bash
### Create the KubeVirt CR (instance deployment request) which triggers the actual installation
kubectl apply -f https://github.com/kubevirt/kubevirt/releases/download/${RELEASE}/kubevirt-cr.yaml
```
``` bash
### wait until all KubeVirt components are up
kubectl -n kubevirt wait kv kubevirt --for condition=Available
```

### **5. Deploy Operators**
Vai nella cartella argocd, dopodichè esegui:
``` bash
kubectl apply -f servicerequest-crd.yaml
```
Vai nella cartella kubebuilder-project, dopodichè esegui
``` bash
make build
make run
```

### **6. Start using VMs with public IPs**
Per utilizzare il progetto, puoi creare una VM con IP pubblico. Puoi farlo attraverso il file "service-request.yaml" presente nella cartella argocd, oppure attraverso il controller creato con kubebuilder.
Il file service-request.yaml è un esempio di richiesta di creazione di una VM con un indirizzo IP pubblico associato. Puoi modificarlo secondo le tue esigenze.
In particolare il file è ideato per:
- avere una VM e passare quindi a un caso concreto
- esporre 2 servizi e quindi verificare che ciò sia possibile anche per metallb
- essere riapplicato più volte e verificare che non ci siano problemi di condivisione dell'IP per utilizzo dello stesso servizio

#### Esempio di utilizzo del file service-request.yaml:
``` bash
kubectl apply -f service-request.yaml
```

### **7. Verifica delle risorse create**
Puoi verificare le risorse create utilizzando i seguenti comandi:
``` bash
kubectl get vm --all-namespaces
kubectl get service --all-namespaces
```
### Esempio di output:
``` bash
 kubectl get vm --all-namespaces
NAMESPACE   NAME          AGE     STATUS     READY
ns1         testvm-app2   7m11s   Starting   False

 kubectl get service -n ns1
NAME                             TYPE           CLUSTER-IP     EXTERNAL-IP    PORT(S)                           AGE
service-myservice-request-app2   LoadBalancer   10.96.176.32   172.18.0.240   30026:32212/TCP,30027:30505/