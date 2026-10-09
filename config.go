package main

// playbackConfig is the settings block KANALI sends, in the same field order.
func playbackConfig(brightness int, remote string) kanaliUserConfig {
	if brightness <= 0 {
		brightness = 80
	}
	var cfg kanaliUserConfig
	cfg.Poweron.MediaFile = "default_poweron_1280x720.mp4.h264"
	cfg.Standby.Enable = true
	cfg.Standby.MediaFile = "default_standby_1280x720.mp4.h264"
	cfg.Work.MediaMode = "MediaMode_Single"
	cfg.Work.Single = remote
	cfg.Work.Loop = "LoopMode_Single"
	cfg.Display.Brightness = brightness
	cfg.Display.Enable = true
	return cfg
}

// kanaliUserConfig field order matches the payload that actually changes the screen.
type kanaliUserConfig struct {
	Poweron struct {
		MediaFile string `json:"mediaFile"`
	} `json:"poweronConfig"`
	Standby struct {
		Enable    bool   `json:"enable"`
		MediaFile string `json:"mediaFile"`
	} `json:"standbyConfig"`
	Work struct {
		MediaMode string `json:"mediaMode"`
		Single    string `json:"singleModeMediaFile"`
		Left      string `json:"dualModeLeftMediaFile"`
		Right     string `json:"dualModeRightMediaFile"`
		Loop      string `json:"loopMode"`
	} `json:"workConfig"`
	Display struct {
		Brightness int  `json:"backlightBrightness"`
		Enable     bool `json:"backlightEnable"`
	} `json:"displayConfig"`
}

func configMap(root map[string]any, key string) (map[string]any, bool) {
	if root == nil {
		return nil, false
	}
	child, ok := root[key].(map[string]any)
	return child, ok && child != nil
}
