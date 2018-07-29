# Graphite Exporter for Go

Getting your data into Graphite is very flexible.

Graphite is a real-time graphing system that stores numeric time-series data and renders graphs of the received data on demand.

## Options for the Graphite exporter

In this exporter, there are some options that can be defined when registering and creating the exporter. The list of options are shown in the table below:

| Field | Description | Default Value |
| ------ | ------ | ------ |
| Host | Type `string`. The Host contains the host address for the graphite server | "127.0.0.1" |
| Port | Type `int`. The Port in which the carbon/graphite endpoint is available | 2003
| Namespace | Type `string`. The Namespace is a string value to build the metric path. It will be the first value on the path | None |
| ReportingPeriod | Type `time.Duration`. The ReportingPeriod is a value to determine the buffer timeframe in which the stats data will be sent. | 1 second |


## Implementation Details

The format to feed data into Graphite in Plaintext is `<metric path> <metric value> <metric timestamp>`.

  - `metric_path` is the metric namespace.
  - `value` is the value of the metric at a given time.
  - `timestamp` is the number of seconds since unix epoch time and the time in which the data is received on Graphite.

## How the stats data is handled?

In this exporter the stats data is aggregated into Views (which are essentially a collection of metrics, each with a different set of labels). To know more about the definition of views, check the [Opencensus docs](https://github.com/census-instrumentation/opencensus-specs/blob/master/stats/Export.md)

## How the path is built?

One of the main concepts of Graphite is the `metric path`. This path is used to aggregate and organize the measurements and generate the graphs.

In this exporter, the path is built as follows:

`Options.Namespace'.'View.Name`.'Tags'

  - `Options.Namespace`: Defined in the 'Options' object.
  - `View.Name`: The name given to the view.
  - `Tags`: The view tag key and values in the format `key_value`
  - `Measure.Name`: The name given to the measure


For example, in a configuration where:

  - `Options.Namespace` = 'opencensus'
  - `View.Name`: 'video'
  - `Tags`: { "name": "video1", "author": "john"}
  - `Measure.Name`: 'size'

`opencensus.video.name_video1.author_john.size`