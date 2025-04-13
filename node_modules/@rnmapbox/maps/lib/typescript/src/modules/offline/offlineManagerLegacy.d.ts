export { default as OfflineCreatePackOptions, type OfflineCreatePackOptionsArgs, } from './OfflineCreatePackOptions';
import { type OfflineCreatePackOptionsArgs } from './OfflineCreatePackOptions';
import OfflinePack from './OfflinePackLegacy';
/**
 * OfflineManagerLegacy implements a singleton (shared object) that manages offline packs.
 * All of this class’s instance methods are asynchronous, reflecting the fact that offline resources are stored in a database.
 * The shared object maintains a canonical collection of offline packs.
 */
declare class OfflineManagerLegacy {
    private _hasInitialized;
    private _offlinePacks;
    constructor();
    /**
     * Creates and registers an offline pack that downloads the resources needed to use the given region offline.
     *
     * @example
     *
     * await Mapbox.offlineManager.createPack({
     *   name: 'offlinePack',
     *   styleURL: 'mapbox://...',
     *   minZoom: 14,
     *   maxZoom: 20,
     *   bounds: [[neLng, neLat], [swLng, swLat]]
     * })
     *
     * @param  {OfflineCreatePackOptions} options Create options for a offline pack that specifices zoom levels, style url, and the region to download.
     * @return {void}
     */
    createPack(options: OfflineCreatePackOptionsArgs): Promise<void>;
    /**
     * Invalidates the specified offline pack. This method checks that the tiles in the specified offline pack match those from the server. Local tiles that do not match the latest version on the server are updated.
     *
     * This is more efficient than deleting the offline pack and downloading it again. If the data stored locally matches that on the server, new data will not be downloaded.
     *
     * @example
     * await Mapbox.offlineManagerLegacy.invalidatePack('packName')
     *
     * @param  {String}  name  Name of the offline pack.
     * @return {void}
     */
    invalidatePack(name: string): Promise<void>;
    /**
     * Unregisters the given offline pack and allows resources that are no longer required by any remaining packs to be potentially freed.
     *
     * @example
     * await Mapbox.offlineManagerLegacy.deletePack('packName')
     *
     * @param  {String}  name  Name of the offline pack.
     * @return {void}
     */
    deletePack(name: string): Promise<void>;
    /**
     * Migrates the offline cache from pre-v10 SDKs to the new v10 cache location
     *
     * @example
     * await Mapbox.offlineManager.migrateOfflineCache()
     *
     * @return {void}
     */
    migrateOfflineCache(): Promise<void>;
    /**
     * Deletes the existing database, which includes both the ambient cache and offline packs, then reinitializes it.
     *
     * @example
     * await Mapbox.offlineManager.resetDatabase();
     *
     * @return {void}
     */
    resetDatabase(): Promise<void>;
    /**
     * Retrieves all the current offline packs that are stored in the database.
     *
     * @example
     * const offlinePacks = await Mapbox.offlineManagerLegacy.getPacks();
     *
     * @return {Array<OfflinePack>}
     */
    getPacks(): Promise<OfflinePack[]>;
    /**
     * Retrieves an offline pack that is stored in the database by name.
     *
     * @example
     * const offlinePack = await Mapbox.offlineManagerLegacy.getPack();
     *
     * @param  {String}  name  Name of the offline pack.
     * @return {OfflinePack}
     */
    getPack(name: string): Promise<OfflinePack | undefined>;
    _initialize(forceInit?: boolean): Promise<boolean>;
}
declare const offlineManagerLegacy: OfflineManagerLegacy;
export default offlineManagerLegacy;
//# sourceMappingURL=offlineManagerLegacy.d.ts.map