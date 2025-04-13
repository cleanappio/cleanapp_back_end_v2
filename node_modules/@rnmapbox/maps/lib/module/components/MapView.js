"use strict";

import React from 'react';
import { View, StyleSheet, NativeModules } from 'react-native';
import { debounce } from 'debounce';
import NativeMapView from '../specs/RNMBXMapViewNativeComponent';
import NativeMapViewModule from '../specs/NativeMapViewModule';
import { isFunction, isAndroid } from '../utils';
import { getFilter } from '../utils/filterUtils';
import Logger from '../utils/Logger';
import NativeBridgeComponent from './NativeBridgeComponent';
import { jsx as _jsx } from "react/jsx-runtime";
const {
  RNMBXModule
} = NativeModules;
const {
  EventTypes
} = RNMBXModule;
if (RNMBXModule == null) {
  console.error('Native part of Mapbox React Native libraries were not registered properly, double check our native installation guides.');
}
if (!RNMBXModule.MapboxV10) {
  console.warn('@rnmapbox/maps: Non v10 implementations are deprecated and will be removed in next version - see https://github.com/rnmapbox/maps/wiki/Deprecated-RNMapboxImpl-Maplibre');
}
const styles = StyleSheet.create({
  matchParent: {
    flex: 1
  }
});
const defaultStyleURL = RNMBXModule.StyleURL.Street;

/**
 * v10 only
 */

/**
 * label localization settings (v10 only). `true` is equivalent to current locale.
 */

/**
 * MapView backed by Mapbox Native GL
 */
class MapView extends NativeBridgeComponent(React.PureComponent, NativeMapViewModule) {
  static defaultProps = {
    scrollEnabled: true,
    pitchEnabled: true,
    rotateEnabled: true,
    attributionEnabled: true,
    compassEnabled: false,
    compassFadeWhenNorth: false,
    logoEnabled: true,
    scaleBarEnabled: true,
    surfaceView: RNMBXModule.MapboxV10 ? true : false,
    requestDisallowInterceptTouchEvent: false,
    regionWillChangeDebounceTime: 10,
    regionDidChangeDebounceTime: 500
  };
  deprecationLogged = {
    contentInset: false,
    regionDidChange: false,
    regionIsChanging: false
  };
  constructor(props) {
    super(props);
    this.logger = Logger.sharedInstance();
    this.logger.start();
    this.state = {
      isReady: null,
      region: null,
      width: 0,
      height: 0,
      isUserInteraction: false
    };
    this._onPress = this._onPress.bind(this);
    this._onLongPress = this._onLongPress.bind(this);
    this._onChange = this._onChange.bind(this);
    this._onLayout = this._onLayout.bind(this);
    this._onCameraChanged = this._onCameraChanged.bind(this);

    // debounced map change methods
    this._onDebouncedRegionWillChange = debounce(this._onRegionWillChange.bind(this), props.regionWillChangeDebounceTime, true);
    this._onDebouncedRegionDidChange = debounce(this._onRegionDidChange.bind(this), props.regionDidChangeDebounceTime);
  }
  componentDidMount() {
    this._setHandledMapChangedEvents(this.props);
  }
  componentWillUnmount() {
    this._onDebouncedRegionWillChange.clear();
    this._onDebouncedRegionDidChange.clear();
    this.logger.stop();
  }
  UNSAFE_componentWillReceiveProps(nextProps) {
    this._setHandledMapChangedEvents(nextProps);
  }
  _setHandledMapChangedEvents(props) {
    if (isAndroid() || RNMBXModule.MapboxV10) {
      const events = [];
      function addIfHasHandler(name) {
        if (props[`on${name}`] != null) {
          if (EventTypes[name] == null) {
            if (name === 'DidFailLoadingMap') {
              console.warn(`rnmapbox maps: on${name} is deprecated, please use onMapLoadingError`);
            } else {
              console.warn(`rnmapbox maps: ${name} is not supported`);
            }
          } else {
            events.push(EventTypes[name]);
            return true;
          }
        }
        return false;
      }
      addIfHasHandler('RegionWillChange');
      addIfHasHandler('RegionIsChanging');
      addIfHasHandler('RegionDidChange');
      addIfHasHandler('UserLocationUpdate');
      addIfHasHandler('WillStartLoadingMap');
      addIfHasHandler('DidFinishLoadingMap');
      addIfHasHandler('MapLoadingError');
      addIfHasHandler('DidFailLoadingMap');
      addIfHasHandler('WillStartRenderingFrame');
      addIfHasHandler('DidFinishRenderingFrame');
      addIfHasHandler('DidFinishRenderingFrameFully');
      addIfHasHandler('WillStartRenderingMap');
      addIfHasHandler('DidFinishRenderingMap');
      addIfHasHandler('DidFinishRenderingMapFully');
      addIfHasHandler('DidFinishLoadingStyle');
      addIfHasHandler('CameraChanged');
      addIfHasHandler('MapIdle');
      if (addIfHasHandler('RegionDidChange')) {
        if (!this.deprecationLogged.regionDidChange) {
          console.warn('onRegionDidChange is deprecated and will be removed in next release - please use onMapIdle. https://github.com/rnmapbox/maps/wiki/Deprecated-RegionIsDidChange');
          this.deprecationLogged.regionDidChange = true;
        }
        if (props.onMapIdle) {
          console.warn('rnmapbox/maps: only one of MapView.onRegionDidChange or onMapIdle is supported');
        }
      }
      if (addIfHasHandler('RegionIsChanging')) {
        if (!this.deprecationLogged.regionIsChanging) {
          console.warn('onRegionIsChanging is deprecated and will be removed in next release - please use onCameraChanged. https://github.com/rnmapbox/maps/wiki/Deprecated-RegionIsDidChange');
          this.deprecationLogged.regionIsChanging = true;
        }
        if (props.onCameraChanged) {
          console.warn('rnmapbox/maps: only one of MapView.onRegionIsChanging or onCameraChanged is supported');
        }
      }
      if (props.onRegionWillChange) {
        console.warn('onRegionWillChange is deprecated and will be removed in v10 - please use onRegionIsChanging');
      }
      this._runNativeMethod('setHandledMapChangedEvents', this._nativeRef, [events]);
    }
  }

  /**
   * Converts a geographic coordinate to a point in the given view’s coordinate system.
   *
   * @example
   * const pointInView = await this._map.getPointInView([-37.817070, 144.949901]);
   *
   * @param {Array<number>} coordinate - A point expressed in the map view's coordinate system.
   * @return {Array}
   */
  async getPointInView(coordinate) {
    const res = await this._runNative('getPointInView', [coordinate]);
    return res.pointInView;
  }

  /**
   * Converts a point in the given view’s coordinate system to a geographic coordinate.
   *
   * @example
   * const coordinate = await this._map.getCoordinateFromView([100, 100]);
   *
   * @param {Array<number>} point - A point expressed in the given view’s coordinate system.
   * @return {Array}
   */
  async getCoordinateFromView(point) {
    const res = await this._runNative('getCoordinateFromView', [point]);
    return res.coordinateFromView;
  }

  /**
   * The coordinate bounds (ne, sw) visible in the user’s viewport.
   *
   * @example
   * const visibleBounds = await this._map.getVisibleBounds();
   *
   * @return {Array}
   */
  async getVisibleBounds() {
    const res = await this._runNative('getVisibleBounds');
    return res.visibleBounds;
  }

  /**
   * Returns an array of rendered map features that intersect with a given point.
   *
   * @example
   * this._map.queryRenderedFeaturesAtPoint([30, 40], ['==', 'type', 'Point'], ['id1', 'id2'])
   *
   * @param  {Array<Number>} coordinate - A point expressed in the map view’s coordinate system.
   * @param  {Array=} filter - A set of strings that correspond to the names of layers defined in the current style. Only the features contained in these layers are included in the returned array.
   * @param  {Array=} layerIDs - A array of layer id's to filter the features by
   * @return {FeatureCollection}
   */

  async queryRenderedFeaturesAtPoint(coordinate, filter = [], layerIDs = []) {
    if (!coordinate || coordinate.length < 2) {
      throw new Error('Must pass in valid coordinate[lng, lat]');
    }
    const res = await this._runNative('queryRenderedFeaturesAtPoint', [coordinate, getFilter(filter), layerIDs]);
    if (isAndroid()) {
      return JSON.parse(res.data);
    }
    return res.data;
  }

  /**
   * Returns an array of rendered map features that intersect with the given rectangle,
   * restricted to the given style layers and filtered by the given predicate. In v10,
   * passing an empty array will query the entire visible bounds of the map.
   *
   * @example
   * this._map.queryRenderedFeaturesInRect([30, 40, 20, 10], ['==', 'type', 'Point'], ['id1', 'id2'])
   *
   * @param  {Array<Number>} bbox - A rectangle expressed in the map view’s coordinate system. For v10, this can be an empty array to query the visible map area.
   * @param  {Array=} filter - A set of strings that correspond to the names of layers defined in the current style. Only the features contained in these layers are included in the returned array.
   * @param  {Array=} layerIDs -  A array of layer id's to filter the features by
   * @return {FeatureCollection}
   */
  async queryRenderedFeaturesInRect(bbox, filter = [], layerIDs = null) {
    if (bbox != null && (bbox.length === 4 || RNMBXModule.MapboxV10 && bbox.length === 0)) {
      const res = await this._runNative('queryRenderedFeaturesInRect', [bbox, getFilter(filter), layerIDs]);
      if (isAndroid()) {
        return JSON.parse(res.data);
      }
      return res.data;
    } else {
      throw new Error('Must pass in a valid bounding box: [top, right, bottom, left]. An empty array [] is also acceptable in v10.');
    }
  }

  /**
   * Returns an array of GeoJSON Feature objects representing features within the specified vector tile or GeoJSON source that satisfy the query parameters.
   *
   * @example
   * this._map.querySourceFeatures('your-source-id', [], ['your-source-layer'])
   *
   * @param  {String} sourceId - Style source identifier used to query for source features.
   * @param  {Array=} filter - A filter to limit query results.
   * @param  {Array=} sourceLayerIDs - The name of the source layers to query. For vector tile sources, this parameter is required. For GeoJSON sources, it is ignored.
   * @return {FeatureCollection}
   */
  async querySourceFeatures(sourceId, filter = [], sourceLayerIDs = []) {
    const args = [sourceId, getFilter(filter), sourceLayerIDs];
    const res = await this._runNative('querySourceFeatures', args);
    if (isAndroid()) {
      return JSON.parse(res.data);
    }
    return res.data;
  }

  /**
   * Map camera will perform updates based on provided config. Deprecated use Camera#setCamera.
   * @deprecated use Camera#setCamera
   */
  setCamera() {
    console.warn('MapView.setCamera is deprecated - please use Camera#setCamera');
  }
  _runNative(methodName, args = []) {
    return super._runNativeMethod(methodName,
    // eslint-disable-next-line @typescript-eslint/ban-ts-comment
    // @ts-ignore TODO: fix types
    this._nativeRef, args);
  }

  /**
   * Takes snapshot of map with current tiles and returns a URI to the image
   * @param  {Boolean} writeToDisk If true will create a temp file, otherwise it is in base64
   * @return {String}
   */
  async takeSnap(writeToDisk = false) {
    const res = await this._runNative('takeSnap', [writeToDisk]);
    return res.uri;
  }

  /**
   * Returns the current zoom of the map view.
   *
   * @example
   * const zoom = await this._map.getZoom();
   *
   * @return {Number}
   */

  async getZoom() {
    const res = await this._runNative('getZoom');
    return res.zoom;
  }

  /**
   * Returns the map's geographical centerpoint
   *
   * @example
   * const center = await this._map.getCenter();
   *
   * @return {Array<Number>} Coordinates
   */
  async getCenter() {
    const res = await this._runNative('getCenter');
    return res.center;
  }

  /**
   *
   * Clears temporary map data from the data path defined in the given resource
   * options. Useful to reduce the disk usage or in case the disk cache contains
   * invalid data.
   *
   * v10 only
   */
  async clearData() {
    if (!RNMBXModule.MapboxV10) {
      console.warn('RNMapbox: clearData is only implemented in v10 implementation or later');
      return;
    }
    await this._runNative('clearData');
  }

  /**
   * Queries the currently loaded data for elevation at a geographical location.
   * The elevation is returned in meters relative to mean sea-level.
   * Returns null if terrain is disabled or if terrain data for the location hasn't been loaded yet.
   *
   * @param {Array<Number>} coordinate - the coordinates to query elevation at
   * @return {Number}
   */
  async queryTerrainElevation(coordinate) {
    const res = await this._runNative('queryTerrainElevation', [coordinate]);
    return res.data;
  }

  /**
   * Sets the visibility of all the layers referencing the specified `sourceLayerId` and/or `sourceId`
   *
   * @example
   * await this._map.setSourceVisibility(false, 'composite', 'building')
   *
   * @param {boolean} visible - Visibility of the layers
   * @param {String} sourceId - Identifier of the target source (e.g. 'composite')
   * @param {String=} sourceLayerId - Identifier of the target source-layer (e.g. 'building')
   */
  setSourceVisibility(visible, sourceId, sourceLayerId = null) {
    this._runNative('setSourceVisibility', [visible, sourceId, sourceLayerId]);
  }
  _decodePayload(payload) {
    if (typeof payload === 'string') {
      return JSON.parse(payload);
    } else {
      return payload;
    }
  }
  _onPress(e) {
    if (isFunction(this.props.onPress)) {
      this.props.onPress(this._decodePayload(e.nativeEvent.payload));
    }
  }
  _onLongPress(e) {
    if (isFunction(this.props.onLongPress)) {
      this.props.onLongPress(this._decodePayload(e.nativeEvent.payload));
    }
  }
  _onRegionWillChange(payload) {
    if (isFunction(this.props.onRegionWillChange)) {
      this.props.onRegionWillChange(payload);
    }
    this.setState({
      isUserInteraction: payload.properties.isUserInteraction,
      isAnimatingFromUserInteraction: payload.properties.isAnimatingFromUserInteraction
    });
  }
  _onRegionDidChange(payload) {
    if (isFunction(this.props.onRegionDidChange)) {
      this.props.onRegionDidChange(payload);
    }
    this.setState({
      region: payload
    });
  }
  _onCameraChanged(e) {
    this.props.onCameraChanged?.(this._decodePayload(e.nativeEvent.payload));
  }
  _onChange(e) {
    const {
      regionWillChangeDebounceTime,
      regionDidChangeDebounceTime
    } = this.props;
    const {
      type
    } = e.nativeEvent;
    const payload = this._decodePayload(e.nativeEvent.payload);
    let propName = '';
    let deprecatedPropName = '';
    switch (type) {
      case EventTypes.RegionWillChange:
        if (regionWillChangeDebounceTime && regionWillChangeDebounceTime > 0) {
          this._onDebouncedRegionWillChange(payload);
        } else {
          propName = 'onRegionWillChange';
        }
        break;
      case EventTypes.RegionIsChanging:
        propName = 'onRegionIsChanging';
        break;
      case EventTypes.RegionDidChange:
        if (regionDidChangeDebounceTime && regionDidChangeDebounceTime > 0) {
          this._onDebouncedRegionDidChange(payload);
        } else {
          propName = 'onRegionDidChange';
        }
        break;
      case EventTypes.CameraChanged:
        propName = 'onCameraChanged';
        break;
      case EventTypes.MapIdle:
        propName = 'onMapIdle';
        break;
      case EventTypes.UserLocationUpdated:
        propName = 'onUserLocationUpdate';
        break;
      case EventTypes.WillStartLoadingMap:
        propName = 'onWillStartLoadingMap';
        break;
      case EventTypes.DidFinishLoadingMap:
        propName = 'onDidFinishLoadingMap';
        break;
      case EventTypes.DidFailLoadingMap:
        propName = 'onDidFailLoadingMap';
        break;
      case EventTypes.MapLoadingError:
        propName = 'onMapLoadingError';
        deprecatedPropName = 'onDidFailLoadingMap';
        break;
      case EventTypes.WillStartRenderingFrame:
        propName = 'onWillStartRenderingFrame';
        break;
      case EventTypes.DidFinishRenderingFrame:
        propName = 'onDidFinishRenderingFrame';
        break;
      case EventTypes.DidFinishRenderingFrameFully:
        propName = 'onDidFinishRenderingFrameFully';
        break;
      case EventTypes.WillStartRenderingMap:
        propName = 'onWillStartRenderingMap';
        break;
      case EventTypes.DidFinishRenderingMap:
        propName = 'onDidFinishRenderingMap';
        break;
      case EventTypes.DidFinishRenderingMapFully:
        propName = 'onDidFinishRenderingMapFully';
        break;
      case EventTypes.DidFinishLoadingStyle:
        propName = 'onDidFinishLoadingStyle';
        break;
      default:
        console.warn('Unhandled event callback type', type);
    }
    if (propName !== '') {
      this._handleOnChange(propName, payload);
    }
    if (deprecatedPropName !== '') {
      this._handleOnChange(deprecatedPropName, payload);
    }
  }
  _onLayout(e) {
    this.setState({
      isReady: true,
      width: e.nativeEvent.layout.width,
      height: e.nativeEvent.layout.height
    });
  }
  _handleOnChange(propName, payload) {
    const func = this.props[propName];
    if (func && isFunction(func)) {
      func(payload);
    }
  }
  _getContentInset() {
    if (!this.props.contentInset) {
      return;
    }
    if (RNMBXModule.MapboxV10) {
      if (!this.deprecationLogged.contentInset) {
        console.warn('@rnmapbox/maps: contentInset is deprecated, use Camera padding instead.');
        this.deprecationLogged.contentInset = true;
      }
    }
    if (!Array.isArray(this.props.contentInset)) {
      return [this.props.contentInset];
    }
    return this.props.contentInset;
  }
  _setNativeRef(nativeRef) {
    if (nativeRef != null) {
      this._nativeRef = nativeRef;
      super._runPendingNativeMethods(nativeRef);
    }
  }
  setNativeProps(props) {
    if (this._nativeRef) {
      this._nativeRef.setNativeProps(props);
    }
  }
  _setStyleURL(props) {
    // user set a styleURL, no need to alter props
    if (props.styleURL) {
      return;
    }

    // user set styleJSON pass it to styleURL
    if (props.styleJSON && !props.styleURL) {
      props.styleURL = props.styleJSON;
    }

    // user neither set styleJSON nor styleURL
    // set defaultStyleUrl
    if (!props.styleJSON || !props.styleURL) {
      props.styleURL = defaultStyleURL;
    }
  }
  _setLocalizeLabels(props) {
    if (!RNMBXModule.MapboxV10) {
      return;
    }
    if (typeof props.localizeLabels === 'boolean') {
      props.localizeLabels = {
        locale: 'current'
      };
    }
  }
  render() {
    const props = {
      ...this.props,
      contentInset: this._getContentInset(),
      style: styles.matchParent
    };
    this._setStyleURL(props);
    this._setLocalizeLabels(props);
    const callbacks = {
      ref: nativeRef => this._setNativeRef(nativeRef),
      onPress: this._onPress,
      onLongPress: this._onLongPress,
      onMapChange: this._onChange,
      onCameraChanged: this._onCameraChanged
    };
    let mapView = null;
    if (this.state.isReady) {
      if (props._nativeImpl) {
        mapView = /*#__PURE__*/_jsx(props._nativeImpl, {
          ...props,
          ...callbacks
        });
      } else {
        mapView = /*#__PURE__*/_jsx(RNMBXMapView, {
          ...props,
          ...callbacks,
          children: this.props.children
        });
      }
    }
    return /*#__PURE__*/_jsx(View, {
      onLayout: this._onLayout,
      style: this.props.style,
      testID: mapView ? undefined : this.props.testID,
      children: mapView
    });
  }
}
const RNMBXMapView = NativeMapView;
export default MapView;
//# sourceMappingURL=MapView.js.map