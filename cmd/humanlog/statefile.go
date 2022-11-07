package main

type StateFile struct {
	AccountID string `json:"account_id"`
	MachineID string `json:"machine_id"`
}

func readStatefile() (*StateFile, error) {

}

func writeStatefile(sf *StateFile) error {

}
