package browser

import "testing"

func TestInspectCancelFilterEscAfterEnableSideEffect(t *testing.T) {
	var f inspectCancelFilter
	f.set(true)
	if f.onCanceled() {
		t.Fatal("enable-side inspectModeCanceled must not notify UI")
	}
	if f.onCanceled() {
		t.Fatal("second enable-side cancel in the skip window must not notify UI")
	}
	f.expireSkipNow()
	if !f.onCanceled() {
		t.Fatal("Esc after the skip window must notify UI")
	}
}

func TestInspectCancelFilterSwallowsAllCancelsDuringSkip(t *testing.T) {
	var f inspectCancelFilter
	f.set(true)
	for i := 0; i < 3; i++ {
		if f.onCanceled() {
			t.Fatalf("enable-side cancel %d must not notify UI", i)
		}
	}
	f.expireSkipNow()
	if !f.onCanceled() {
		t.Fatal("Esc after skip window must notify UI")
	}
}

func TestInspectCancelFilterNotifiesEscAfterSkipExpires(t *testing.T) {
	var f inspectCancelFilter
	f.set(true)
	f.expireSkipNow()
	if !f.onCanceled() {
		t.Fatal("Esc after skip window must notify UI")
	}
	if f.onCanceled() {
		t.Fatal("second cancel after Esc must not notify again")
	}
}

func TestInspectCancelFilterDisableDoesNotNotify(t *testing.T) {
	var f inspectCancelFilter
	f.set(true)
	f.set(false)
	if f.onCanceled() {
		t.Fatal("toolbar off already cleared UI; Overlay cancel must not echo")
	}
}

func TestInspectCancelFilterNeverEnabled(t *testing.T) {
	var f inspectCancelFilter
	if f.onCanceled() {
		t.Fatal("cancel with inspect never wanted must not notify")
	}
}

func TestInspectCancelFilterReenterArmsNewSkip(t *testing.T) {
	var f inspectCancelFilter
	f.set(true)
	_ = f.onCanceled()
	f.set(true)
	if f.onCanceled() {
		t.Fatal("re-enter must skip the next enable-side cancel")
	}
}

func (f *inspectCancelFilter) expireSkipNow() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.skip = false
}
