"use strict";

Object.defineProperty(exports, "__esModule", {
  value: true
});
Object.defineProperty(exports, "OfflineCreatePackOptions", {
  enumerable: true,
  get: function () {
    return _OfflineCreatePackOptions.default;
  }
});
exports.default = exports.OfflineModuleEventEmitter = void 0;
var _reactNative = require("react-native");
var _utils = require("../../utils");
var _OfflineCreatePackOptions = _interopRequireDefault(require("./OfflineCreatePackOptions"));
var _OfflinePack = _interopRequireDefault(require("./OfflinePack"));
function _interopRequireDefault(e) { return e && e.__esModule ? e : { default: e }; }
const {
  RNMBXModule
} = _reactNative.NativeModules;
const MapboxOfflineManager = _reactNative.NativeModules.RNMBXOfflineModule;
const OfflineModuleEventEmitter = exports.OfflineModuleEventEmitter = new _reactNative.NativeEventEmitter(MapboxOfflineManager);
/**
 * OfflineManager implements a singleton (shared object) that manages offline packs.
 * All of this class’s instance methods are asynchronous, reflecting the fact that offline resources are stored in a database.
 * The shared object maintains a canonical collection of offline packs.
 */
class OfflineManager {
  constructor() {
    this._hasInitialized = false;
    this._offlinePacks = {};
    this._progressListeners = {};
    this._errorListeners = {};
    this._onProgress = this._onProgress.bind(this);
    this._onError = this._onError.bind(this);
    this.subscriptionProgress = null;
    this.subscriptionError = null;
  }

  /**
   * Creates and registers an offline pack that downloads the resources needed to use the given region offline.
   *
   * @example
   *
   * const progressListener = (offlineRegion, status) => console.log(offlineRegion, status);
   * const errorListener = (offlineRegion, err) => console.log(offlineRegion, err);
   *
   * await Mapbox.offlineManager.createPack({
   *   name: 'offlinePack',
   *   styleURL: 'mapbox://...',
   *   minZoom: 14,
   *   maxZoom: 20,
   *   bounds: [[neLng, neLat], [swLng, swLat]]
   * }, progressListener, errorListener)
   *
   * @param  {OfflineCreatePackOptions} options Create options for a offline pack that specifices zoom levels, style url, and the region to download.
   * @param  {Callback=} progressListener Callback that listens for status events while downloading the offline resource.
   * @param  {Callback=} errorListener Callback that listens for status events while downloading the offline resource.
   * @return {void}
   */
  async createPack(options, progressListener, errorListener) {
    await this._initialize();
    const packOptions = new _OfflineCreatePackOptions.default(options);
    if (this._offlinePacks[packOptions.name]) {
      throw new Error(`Offline pack with name ${packOptions.name} already exists.`);
    }
    this.subscribe(packOptions.name, progressListener, errorListener);
    const nativeOfflinePack = await MapboxOfflineManager.createPack(packOptions);
    this._offlinePacks[packOptions.name] = new _OfflinePack.default(nativeOfflinePack);
  }

  /**
   * Invalidates the specified offline pack. This method checks that the tiles in the specified offline pack match those from the server. Local tiles that do not match the latest version on the server are updated.
   *
   * This is more efficient than deleting the offline pack and downloading it again. If the data stored locally matches that on the server, new data will not be downloaded.
   *
   * @example
   * await Mapbox.offlineManager.invalidatePack('packName')
   *
   * @param  {String}  name  Name of the offline pack.
   * @return {void}
   */
  async invalidatePack(name) {
    if (!name) {
      return;
    }
    await this._initialize();
    const offlinePack = this._offlinePacks[name];
    if (offlinePack) {
      await MapboxOfflineManager.invalidatePack(name);
    }
  }

  /**
   * Unregisters the given offline pack and allows resources that are no longer required by any remaining packs to be potentially freed.
   *
   * @example
   * await Mapbox.offlineManager.deletePack('packName')
   *
   * @param  {String}  name  Name of the offline pack.
   * @return {void}
   */
  async deletePack(name) {
    if (!name) {
      return;
    }
    await this._initialize();
    const offlinePack = this._offlinePacks[name];
    if (offlinePack) {
      await MapboxOfflineManager.deletePack(name);
      delete this._offlinePacks[name];
    }
  }

  /**
   * Forces a revalidation of the tiles in the ambient cache and downloads a fresh version of the tiles from the tile server.
   * This is the recommend method for clearing the cache.
   * This is the most efficient method because tiles in the ambient cache are re-downloaded to remove outdated data from a device.
   * It does not erase resources from the ambient cache or delete the database, which can be computationally expensive operations that may carry unintended side effects.
   *
   * @deprecated Not implemented in v10
   *
   * @example
   * await Mapbox.offlineManager.invalidateAmbientCache();
   *
   * @return {void}
   */
  async invalidateAmbientCache() {
    if (RNMBXModule.MapboxV10) {
      console.warn('RNMapbox: invalidateAmbientCache is not implemented on v10');
      return;
    }
    await this._initialize();
    await MapboxOfflineManager.invalidateAmbientCache();
  }

  /**
   * Erases resources from the ambient cache.
   * This method clears the cache and decreases the amount of space that map resources take up on the device.
   *
   * @deprecated Not implemented in v10
   *
   * @example
   * await Mapbox.offlineManager.clearAmbientCache();
   *
   * @return {void}
   */
  async clearAmbientCache() {
    if (RNMBXModule.MapboxV10) {
      console.warn('RNMapbox: clearAmbientCache is not implemented on v10');
      return;
    }
    await this._initialize();
    await MapboxOfflineManager.clearAmbientCache();
  }

  /**
   * Migrates the offline cache from pre-v10 SDKs to the new v10 cache location
   *
   * @example
   * await Mapbox.offlineManager.migrateOfflineCache()
   *
   * @return {void}
   */
  async migrateOfflineCache() {
    await MapboxOfflineManager.migrateOfflineCache();
  }

  /**
   * Sets the maximum size of the ambient cache in bytes. Disables the ambient cache if set to 0.
   * This method may be computationally expensive because it will erase resources from the ambient cache if its size is decreased.
   *
   * @example
   * await Mapbox.offlineManager.setMaximumAmbientCacheSize(5000000);
   *
   * @param  {Number}  size  Size of ambient cache.
   * @return {void}
   */
  async setMaximumAmbientCacheSize(size) {
    await this._initialize();
    await MapboxOfflineManager.setMaximumAmbientCacheSize(size);
  }

  /**
   * Deletes the existing database, which includes both the ambient cache and offline packs, then reinitializes it.
   *
   * @example
   * await Mapbox.offlineManager.resetDatabase();
   *
   * @return {void}
   */
  async resetDatabase() {
    await MapboxOfflineManager.resetDatabase();
    this._offlinePacks = {};
    await this._initialize(true);
  }

  /**
   * Retrieves all the current offline packs that are stored in the database.
   *
   * @example
   * const offlinePacks = await Mapbox.offlineManager.getPacks();
   *
   * @return {Array<OfflinePack>}
   */
  async getPacks() {
    await this._initialize(true);
    return Object.keys(this._offlinePacks).map(name => this._offlinePacks[name]);
  }

  /**
   * Retrieves an offline pack that is stored in the database by name.
   *
   * @example
   * const offlinePack = await Mapbox.offlineManager.getPack();
   *
   * @param  {String}  name  Name of the offline pack.
   * @return {OfflinePack}
   */
  async getPack(name) {
    await this._initialize(true);
    return this._offlinePacks[name];
  }

  /**
   * Sideloads offline db
   *
   * @example
   * await Mapbox.offlineManager.mergeOfflineRegions(path);
   *
   * @param {String} path Path to offline tile db on file system.
   * @return {void}
   */
  async mergeOfflineRegions(path) {
    await this._initialize();
    return MapboxOfflineManager.mergeOfflineRegions(path);
  }

  /**
   * Sets the maximum number of Mapbox-hosted tiles that may be downloaded and stored on the current device.
   * The Mapbox Terms of Service prohibit changing or bypassing this limit without permission from Mapbox.
   *
   * @example
   * Mapbox.offlineManager.setTileCountLimit(1000);
   *
   * @param {Number} limit Map tile limit count.
   * @return {void}
   */
  setTileCountLimit(limit) {
    MapboxOfflineManager.setTileCountLimit(limit);
  }

  /**
   * Sets the period at which download status events will be sent over the React Native bridge.
   * The default is 300ms.
   *
   * @example
   * Mapbox.offlineManager.setProgressEventThrottle(500);
   *
   * @param {Number} throttleValue event throttle value in ms.
   * @return {void}
   */
  setProgressEventThrottle(throttleValue) {
    MapboxOfflineManager.setProgressEventThrottle(throttleValue);
  }

  /**
   * Subscribe to download status/error events for the requested offline pack.
   * Note that createPack calls this internally if listeners are provided.
   *
   * @example
   * const progressListener = (offlinePack, status) => console.log(offlinePack, status)
   * const errorListener = (offlinePack, err) => console.log(offlinePack, err)
   * Mapbox.offlineManager.subscribe('packName', progressListener, errorListener)
   *
   * @param  {String} packName           Name of the offline pack.
   * @param  {Callback} progressListener Callback that listens for status events while downloading the offline resource.
   * @param  {Callback} errorListener      Callback that listens for status events while downloading the offline resource.
   * @return {void}
   */
  async subscribe(packName, progressListener, errorListener) {
    const totalProgressListeners = Object.keys(this._progressListeners).length;
    if ((0, _utils.isFunction)(progressListener)) {
      if (totalProgressListeners === 0) {
        this.subscriptionProgress = OfflineModuleEventEmitter.addListener(RNMBXModule.OfflineCallbackName.Progress, this._onProgress);
      }
      this._progressListeners[packName] = progressListener;
    }
    const totalErrorListeners = Object.keys(this._errorListeners).length;
    if ((0, _utils.isFunction)(errorListener)) {
      if (totalErrorListeners === 0) {
        this.subscriptionError = OfflineModuleEventEmitter.addListener(RNMBXModule.OfflineCallbackName.Error, this._onError);
      }
      this._errorListeners[packName] = errorListener;
    }

    // we need to manually set the pack observer on Android
    // if we're resuming a pack download instead of going thru the create flow
    if ((0, _utils.isAndroid)() && this._offlinePacks[packName]) {
      try {
        // manually set a listener, since listeners are only set on create flow
        await MapboxOfflineManager.setPackObserver(packName);
      } catch (e) {
        console.log('Unable to set pack observer', e);
      }
    }
  }

  /**
   * Unsubscribes any listeners associated with the offline pack.
   * It's a good idea to call this on componentWillUnmount.
   *
   * @example
   * Mapbox.offlineManager.unsubscribe('packName')
   *
   * @param  {String} packName Name of the offline pack.
   * @return {void}
   */
  unsubscribe(packName) {
    delete this._progressListeners[packName];
    delete this._errorListeners[packName];
    if (Object.keys(this._progressListeners).length === 0 && this.subscriptionProgress) {
      this.subscriptionProgress.remove();
    }
    if (Object.keys(this._errorListeners).length === 0 && this.subscriptionError) {
      this.subscriptionError.remove();
    }
  }
  async _initialize(forceInit) {
    if (this._hasInitialized && !forceInit) {
      return true;
    }
    const nativeOfflinePacks = await MapboxOfflineManager.getPacks();
    for (const nativeOfflinePack of nativeOfflinePacks) {
      const offlinePack = new _OfflinePack.default(nativeOfflinePack);
      this._offlinePacks[offlinePack.name] = offlinePack;
    }
    this._hasInitialized = true;
    return true;
  }
  _onProgress(e) {
    const {
      name,
      state
    } = e.payload;
    if (!this._hasListeners(name, this._progressListeners)) {
      return;
    }
    const pack = this._offlinePacks[name];
    this._progressListeners[name](pack, e.payload);

    // cleanup listeners now that they are no longer needed
    if (state === RNMBXModule.OfflinePackDownloadState.Complete) {
      this.unsubscribe(name);
    }
  }
  _onError(e) {
    const {
      name
    } = e.payload;
    if (!this._hasListeners(name, this._errorListeners)) {
      return;
    }
    const pack = this._offlinePacks[name];
    this._errorListeners[name](pack, e.payload);
  }
  _hasListeners(name, listenerMap) {
    return !(0, _utils.isUndefined)(this._offlinePacks[name]) && (0, _utils.isFunction)(listenerMap[name]);
  }
}
const offlineManager = new OfflineManager();
var _default = exports.default = offlineManager;
//# sourceMappingURL=offlineManager.js.map