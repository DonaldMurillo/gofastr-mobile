package mobilehost

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/framework/localapp"
	"github.com/DonaldMurillo/gofastr/framework/owner"
)

type lifecycleBattery struct {
	init  func(*framework.App) error
	start func(context.Context) error
	stop  func(context.Context) error
}

func (*lifecycleBattery) Name() string { return "lifecycle-test" }
func (battery *lifecycleBattery) Init(app *framework.App) error {
	if battery.init != nil {
		return battery.init(app)
	}
	return nil
}
func (battery *lifecycleBattery) OnStart(ctx context.Context) error {
	if battery.start != nil {
		return battery.start(ctx)
	}
	return nil
}
func (battery *lifecycleBattery) OnStop(ctx context.Context) error {
	if battery.stop != nil {
		return battery.stop(ctx)
	}
	return nil
}

func lifecycleHost(t *testing.T, configure func(*framework.App), timeout time.Duration) (*framework.App, *localapp.Data, *Host) {
	t.Helper()
	t.Cleanup(func() { owner.SetExtractor(nil) })
	data, err := localapp.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = data.Close() })
	app := framework.NewApp(Options(data)...)
	app.OnStop(data.Close)
	configure(app)
	host, err := New(app, Config{Data: data, ServiceOnly: true, StartTimeout: timeout})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = host.Stop(ctx)
	})
	return app, data, host
}

func TestServiceOnlyRunsCompleteLifecycleWithoutUI(t *testing.T) {
	var initialized, started, stopped, seeded atomic.Int32
	app, data, host := lifecycleHost(t, func(app *framework.App) {
		app.Entity("early_items", framework.EntityConfig{Fields: []schema.Field{{Name: "title", Type: schema.String}}})
		app.WithSeed(func(ctx context.Context) error {
			seeded.Add(1)
			_, err := app.DB.ExecContext(ctx, "INSERT INTO early_items (id, title) VALUES ('seed', 'ready')")
			return err
		})
		app.RegisterBattery(&lifecycleBattery{
			init: func(app *framework.App) error {
				initialized.Add(1)
				app.Entity("late_items", framework.EntityConfig{Fields: []schema.Field{{Name: "title", Type: schema.String}}})
				return nil
			},
			start: func(ctx context.Context) error {
				started.Add(1)
				_, err := app.DB.ExecContext(ctx, "INSERT INTO late_items (id, title) VALUES ('start', 'ready')")
				return err
			},
			stop: func(ctx context.Context) error {
				stopped.Add(1)
				return app.DB.PingContext(ctx)
			},
		})
		app.Router().Get("/identity", http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
			user, ok := handler.GetUser(request.Context())
			if !ok {
				response.WriteHeader(http.StatusUnauthorized)
				return
			}
			_, _ = response.Write([]byte(user.(*localapp.User).GetID()))
		}))
	}, 0)
	if err := host.Start(); err != nil {
		t.Fatal(err)
	}
	if len(app.Mountables()) != 0 {
		t.Fatal("service-only startup mounted a presentation")
	}
	if host.ln.Addr().Network() != "memory" || host.ln.Addr().String() != "memory" {
		t.Fatalf("service-only transport is not explicitly in-memory: %v", host.ln.Addr())
	}
	response, err := host.Do(context.Background(), &Request{URL: DefaultOrigin + "/identity"})
	if err != nil || response.Status != http.StatusOK || string(response.Body) != data.User.GetID() {
		t.Fatalf("installation identity = %+v, %v", response, err)
	}
	for _, route := range []string{"/", "/notes", bridgeScriptPath} {
		response, err := host.Do(context.Background(), &Request{URL: DefaultOrigin + route})
		if err != nil || response.Status != http.StatusNotFound {
			t.Fatalf("unused UI route %s = %+v, %v", route, response, err)
		}
	}
	if initialized.Load() != 1 || started.Load() != 1 || seeded.Load() != 1 {
		t.Fatalf("startup counts: init=%d start=%d seed=%d", initialized.Load(), started.Load(), seeded.Load())
	}
	for range 2 {
		if err := host.Stop(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if stopped.Load() != 1 {
		t.Fatalf("battery stopped %d times", stopped.Load())
	}
	if err := data.DB.Ping(); err == nil {
		t.Fatal("data survived the app's stop hook")
	}
	if err := host.Start(); err == nil {
		t.Fatal("a stopped host restarted its one-shot App")
	}
}

func TestStartupFailureDrainsAndStopIsRepeatable(t *testing.T) {
	failure := errors.New("startup failed")
	for _, phase := range []string{"early-validation", "migration", "seed", "init", "start"} {
		t.Run(phase, func(t *testing.T) {
			var stopped atomic.Int32
			_, data, host := lifecycleHost(t, func(app *framework.App) {
				app.OnStop(func() error { stopped.Add(1); return nil })
				switch phase {
				case "early-validation":
					app.Config.RequestTimeout = time.Minute
					app.Config.HTTPServerTimeouts.WriteTimeout = new(time.Second)
				case "migration":
					app.Entity("broken", framework.EntityConfig{Fields: []schema.Field{{Name: "title", Type: schema.String}}})
					_ = app.DB.Close()
				case "seed":
					app.WithSeed(func(context.Context) error { return failure })
				case "init":
					app.RegisterBattery(&lifecycleBattery{init: func(*framework.App) error { return failure }})
				case "start":
					app.OnStart(func(context.Context) error { return failure })
				}
			}, 0)
			if err := host.Start(); err == nil {
				t.Fatal("startup failure was hidden")
			} else if phase != "migration" && phase != "early-validation" && !errors.Is(err, failure) {
				t.Fatalf("lost startup cause: %v", err)
			}
			if stopped.Load() != 1 {
				t.Fatalf("failed startup ran %d stop hooks", stopped.Load())
			}
			if err := data.DB.Ping(); err == nil {
				t.Fatal("failed startup leaked the database")
			}
			for range 2 {
				ctx, cancel := context.WithTimeout(context.Background(), time.Second)
				err := host.Stop(ctx)
				cancel()
				if errors.Is(err, context.DeadlineExceeded) {
					t.Fatalf("Stop after failed Start consumed the completion: %v", err)
				}
			}
			if stopped.Load() != 1 {
				t.Fatal("failed startup teardown was replayed")
			}
		})
	}
}

func TestStartupTimeoutCancelsAndJoinsWorkers(t *testing.T) {
	workerStarted := make(chan struct{})
	workerDone := make(chan struct{})
	_, data, host := lifecycleHost(t, func(app *framework.App) {
		app.OnStart(func(ctx context.Context) error {
			close(workerStarted)
			defer close(workerDone)
			<-ctx.Done()
			return ctx.Err()
		})
	}, 100*time.Millisecond)
	if err := host.Start(); err == nil || !strings.Contains(err.Error(), "not ready") {
		t.Fatalf("startup timeout = %v", err)
	}
	select {
	case <-host.done:
	default:
		t.Fatal("startup returned before Serve finished")
	}
	// The deadline also covers migrations. Under load it can expire before
	// OnStart runs, so only a worker that actually started must be joined.
	select {
	case <-workerStarted:
		select {
		case <-workerDone:
		default:
			t.Fatal("startup returned before its worker was cancelled")
		}
	default:
	}
	if err := data.DB.Ping(); err == nil {
		t.Fatal("startup timeout leaked the database")
	}
}

func TestServiceOnlyPreservesForeignAuthenticator(t *testing.T) {
	_, _, host := lifecycleHost(t, func(app *framework.App) {
		owner.SetExtractor(func(context.Context) (any, bool) { return "authenticated", true })
		app.Router().Get("/identity", http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
			if _, ok := handler.GetUser(request.Context()); ok {
				response.WriteHeader(http.StatusForbidden)
				return
			}
			response.WriteHeader(http.StatusNoContent)
		}))
	}, 0)
	if err := host.Start(); err != nil {
		t.Fatal(err)
	}
	response, err := host.Do(context.Background(), &Request{URL: DefaultOrigin + "/identity"})
	if err != nil || response.Status != http.StatusNoContent {
		t.Fatalf("local identity replaced the authenticator: %+v %v", response, err)
	}
}

func TestStopBeforeStartClosesComposition(t *testing.T) {
	_, data, host := lifecycleHost(t, func(*framework.App) {}, 0)
	if err := host.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := data.DB.Ping(); err == nil {
		t.Fatal("abandoned composition leaked its database")
	}
	if err := host.Start(); err == nil {
		t.Fatal("Start admitted an already stopped app")
	}
}

func TestStopDrainsLongRequestWithOneShutdownOwner(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	var stopped atomic.Int32
	_, _, host := lifecycleHost(t, func(app *framework.App) {
		app.OnStop(func() error { stopped.Add(1); return nil })
		app.Router().Get("/held", http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
			close(entered)
			select {
			case <-release:
				response.WriteHeader(http.StatusNoContent)
			case <-request.Context().Done():
			}
		}))
	}, 0)
	t.Cleanup(func() {
		select {
		case <-release:
		default:
			close(release)
		}
	})
	if err := host.Start(); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	requestDone := make(chan error, 1)
	go func() {
		response, err := host.Do(ctx, &Request{URL: DefaultOrigin + "/held"})
		if err == nil && response.Status != http.StatusNoContent {
			err = errors.New("held request did not complete normally")
		}
		requestDone <- err
	}()
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal("request did not reach the app")
	}
	stopDone := make(chan error, 3)
	go func() { stopDone <- host.Stop(ctx) }()
	select {
	case <-host.ln.closed:
	case <-ctx.Done():
		t.Fatal("shutdown did not close the listener")
	}
	for range 2 {
		go func() { stopDone <- host.Stop(ctx) }()
	}
	timer := time.NewTimer(5200 * time.Millisecond)
	defer timer.Stop()
	select {
	case err := <-stopDone:
		t.Fatalf("Stop returned before the request drained: %v", err)
	case <-timer.C:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	close(release)
	if err := <-requestDone; err != nil {
		t.Fatal(err)
	}
	for range 3 {
		if err := <-stopDone; err != nil {
			t.Fatalf("Stop reported a follower timeout after the request drained: %v", err)
		}
	}
	if stopped.Load() != 1 {
		t.Fatalf("shutdown ran %d stop hooks", stopped.Load())
	}
	if err := host.Stop(ctx); err != nil {
		t.Fatalf("repeated Stop = %v", err)
	}
}

func TestStopCancelsOutstandingBridgeRequestsBeforeClosingStorage(t *testing.T) {
	entered := make(chan struct{})
	finished := make(chan struct{})
	_, _, host := lifecycleHost(t, func(app *framework.App) {
		app.Router().Get("/watch", http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
			close(entered)
			<-request.Context().Done()
			close(finished)
		}))
		app.OnStop(func() error {
			select {
			case <-finished:
				return nil
			default:
				return errors.New("bridge handler outlived request drain")
			}
		})
	}, 0)
	if err := host.Start(); err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(BridgeRequest{ID: "held-watch", Method: http.MethodGet, URL: DefaultOrigin + "/watch"})
	if err != nil {
		t.Fatal(err)
	}
	replyDone := make(chan string, 1)
	go func() { replyDone <- host.Bridge(string(encoded)) }()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal("bridge request did not reach the app")
	}
	if err := host.Stop(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case encodedReply := <-replyDone:
		var reply BridgeReply
		if err := json.Unmarshal([]byte(encodedReply), &reply); err != nil || reply.ID != "held-watch" || !strings.Contains(reply.Error, "context canceled") {
			t.Fatalf("cancelled bridge reply = %+v, %v", reply, err)
		}
	case <-ctx.Done():
		t.Fatal("shutdown left a bridge call waiting")
	}
}
