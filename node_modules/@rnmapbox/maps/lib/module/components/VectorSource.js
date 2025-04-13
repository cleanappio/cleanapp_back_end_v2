"use strict";

import React from 'react';
import { NativeModules } from 'react-native';
import RNMBXVectorSourceNativeComponent from '../specs/RNMBXVectorSourceNativeComponent';
import { cloneReactChildrenWithProps, isFunction } from '../utils';
import { copyPropertiesAsDeprecated } from '../utils/deprecation';
import AbstractSource from './AbstractSource';
import { jsx as _jsx } from "react/jsx-runtime";
const MapboxGL = NativeModules.RNMBXModule;
export const NATIVE_MODULE_NAME = 'RNMBXVectorSource';

//interface NativeProps extends Omit<Props, 'children'> {}

// Omit<Props, 'children'>;
/**
 * VectorSource is a map content source that supplies tiled vector data in Mapbox Vector Tile format to be shown on the map.
 * The location of and metadata about the tiles are defined either by an option dictionary or by an external file that conforms to the TileJSON specification.
 */
class VectorSource extends AbstractSource {
  static defaultProps = {
    id: MapboxGL.StyleSource.DefaultSourceID
  };
  constructor(props) {
    super(props);
  }
  _decodePayload(payload) {
    // we check whether the payload is a string, since the strict type safety is enforced only on iOS on the new arch
    // on Android, on both archs, the payload is an object
    if (typeof payload === 'string') {
      return JSON.parse(payload);
    } else {
      return payload;
    }
  }
  onPress(event) {
    const payload = this._decodePayload(event.nativeEvent.payload);
    const {
      features,
      coordinates,
      point
    } = payload;
    let newEvent = {
      features,
      coordinates,
      point
    };
    newEvent = copyPropertiesAsDeprecated(event, newEvent, key => {
      console.warn(`event.${key} is deprecated on VectorSource#onPress, please use event.features`);
    }, {
      nativeEvent: origNativeEvent => ({
        ...origNativeEvent,
        payload: features[0]
      })
    });
    const {
      onPress
    } = this.props;
    if (onPress) {
      onPress(newEvent);
    }
  }
  render() {
    const props = {
      id: this.props.id,
      existing: this.props.existing,
      url: this.props.url,
      tileUrlTemplates: this.props.tileUrlTemplates,
      minZoomLevel: this.props.minZoomLevel,
      maxZoomLevel: this.props.maxZoomLevel,
      tms: this.props.tms,
      attribution: this.props.attribution,
      hitbox: this.props.hitbox,
      hasPressListener: isFunction(this.props.onPress),
      onMapboxVectorSourcePress: this.onPress.bind(this),
      onPress: undefined
    };
    return (
      /*#__PURE__*/
      // @ts-expect-error just codegen stuff
      _jsx(RNMBXVectorSourceNativeComponent, {
        ref: this.setNativeRef,
        ...props,
        children: cloneReactChildrenWithProps(this.props.children, {
          sourceID: this.props.id
        })
      })
    );
  }
}
export default VectorSource;
//# sourceMappingURL=VectorSource.js.map