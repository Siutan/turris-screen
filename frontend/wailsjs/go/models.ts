export namespace main {
	
	export class BridgeStatus {
	    connected: boolean;
	    serial: string;
	    detail: string;
	
	    static createFrom(source: any = {}) {
	        return new BridgeStatus(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.connected = source["connected"];
	        this.serial = source["serial"];
	        this.detail = source["detail"];
	    }
	}
	export class JobProgress {
	    active: boolean;
	    label: string;
	    percent: number;
	
	    static createFrom(source: any = {}) {
	        return new JobProgress(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.active = source["active"];
	        this.label = source["label"];
	        this.percent = source["percent"];
	    }
	}
	export class MediaItem {
	    id: string;
	    name: string;
	    remoteName: string;
	    created: string;
	    thumb: string;
	
	    static createFrom(source: any = {}) {
	        return new MediaItem(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.remoteName = source["remoteName"];
	        this.created = source["created"];
	        this.thumb = source["thumb"];
	    }
	}

}

