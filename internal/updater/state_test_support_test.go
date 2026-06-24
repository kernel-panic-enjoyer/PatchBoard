package updater

import "context"

func saveState(state State) error {
	store, err := defaultStateStore()
	if err != nil {
		return err
	}
	_, err = store.Update(context.Background(), func(current *State) error {
		*current = state
		return nil
	})
	return err
}
