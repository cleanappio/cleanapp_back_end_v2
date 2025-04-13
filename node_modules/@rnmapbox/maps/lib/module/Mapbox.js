"use strict";

export * from './RNMBXModule';
export { Camera, UserTrackingMode } from './components/Camera';
export { Atmosphere } from './components/Atmosphere';
export { default as MapView } from './components/MapView';
export { default as Light } from './components/Light';
export { default as PointAnnotation } from './components/PointAnnotation';
export { default as Annotation } from './components/Annotation';
export { default as Callout } from './components/Callout';
export { default as StyleImport } from './components/StyleImport';
export { default as UserLocation, UserLocationRenderMode } from './components/UserLocation';
export { default as LocationPuck } from './components/LocationPuck';
export { default as VectorSource } from './components/VectorSource';
export { ShapeSource } from './components/ShapeSource';
export { default as RasterSource } from './components/RasterSource';
export { default as RasterDemSource } from './components/RasterDemSource';
export { default as ImageSource } from './components/ImageSource';
export { Viewport } from './components/Viewport';
export { default as Models } from './components/Models';
export { default as Images } from './components/Images';
export { default as Image } from './components/Image';
export { default as FillLayer } from './components/FillLayer';
export { default as FillExtrusionLayer } from './components/FillExtrusionLayer';
export { default as HeatmapLayer } from './components/HeatmapLayer';
export { default as LineLayer } from './components/LineLayer';
export { default as CircleLayer } from './components/CircleLayer';
export { default as SkyLayer } from './components/SkyLayer';
export { default as ModelLayer } from './components/ModelLayer';
export { SymbolLayer } from './components/SymbolLayer';
export { default as RasterLayer } from './components/RasterLayer';
export { default as BackgroundLayer } from './components/BackgroundLayer';
export { default as CustomLocationProvider } from './components/CustomLocationProvider';
export { Terrain } from './components/Terrain';
export { default as locationManager } from './modules/location/locationManager';
export { default as offlineManager, OfflineCreatePackOptions } from './modules/offline/offlineManager';
export { default as offlineManagerLegacy } from './modules/offline/offlineManagerLegacy';
export { default as TileStore } from './modules/offline/TileStore';
export { default as snapshotManager } from './modules/snapshot/snapshotManager';
export { default as MarkerView } from './components/MarkerView';
export { default as Animated } from './utils/animated/Animated';
export { AnimatedCoordinatesArray, AnimatedExtractCoordinateFromArray, AnimatedPoint, AnimatedRouteCoordinatesArray, AnimatedShape } from './classes';
export { default as Style } from './components/Style';
export { default as Logger } from './utils/Logger';
export { requestAndroidLocationPermissions } from './requestAndroidLocationPermissions';
export { getAnnotationsLayerID } from './utils/getAnnotationsLayerID';
import { deprecatedClass } from './utils/deprecation';
import { AnimatedPoint } from './classes';
import { UserTrackingMode } from './components/Camera';
import MovePointShapeAnimator from './shapeAnimators/MovePointShapeAnimator';
import ChangeLineOffsetsShapeAnimator from './shapeAnimators/ChangeLineOffsetsShapeAnimator';
import LocationPuck from './components/LocationPuck';

/** @deprecated This will be removed in a future release. Use `AnimatedPoint` instead. */

export const AnimatedMapPoint = deprecatedClass(AnimatedPoint, 'AnimatedMapPoint is deprecated please use AnimatedPoint');

/** @deprecated NativeUserLocation will be removed in future release. Use `LocationPuck` instead. */
export const NativeUserLocation = LocationPuck;

// types:
export let StyleURL = /*#__PURE__*/function (StyleURL) {
  StyleURL["Street"] = "mapbox://styles/mapbox/streets-v11";
  StyleURL["Dark"] = "mapbox://styles/mapbox/dark-v10";
  StyleURL["Light"] = "mapbox://styles/mapbox/light-v10";
  StyleURL["Outdoors"] = "mapbox://styles/mapbox/outdoors-v11";
  StyleURL["Satellite"] = "mapbox://styles/mapbox/satellite-v9";
  StyleURL["SatelliteStreet"] = "mapbox://styles/mapbox/satellite-streets-v11";
  StyleURL["TrafficDay"] = "mapbox://styles/mapbox/navigation-preview-day-v4";
  StyleURL["TrafficNight"] = "mapbox://styles/mapbox/navigation-preview-night-v4";
  return StyleURL;
}({});

/** @deprecated UserTrackingModes is deprecated use UserTrackingMode */
export const UserTrackingModes = UserTrackingMode;

/** @experimental */

export const __experimental = {
  MovePointShapeAnimator,
  ChangeLineOffsetsShapeAnimator
};
//# sourceMappingURL=Mapbox.js.map